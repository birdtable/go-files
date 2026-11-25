//go:build unix

package confine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Mkdir sets its mode exactly on each folder it makes, whatever the umask, keeps a setgid bit a
// folder takes from the one above, and leaves a folder that was there as it was.
//
// What it costs when this fails: a folder for a run's secrets is made 0755 under a umask of 022,
// or a shared folder above it is narrowed.
func TestMkdirSetsItsModeExactlyOnEachFolderItMakes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "a"), 0o711); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "a"), 0o711); err != nil {
		t.Fatal(err)
	}
	root := rootAt(t, dir)
	r, err := Mkdir(root, filepath.Join("a", "b", "c"), 0o777)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	for d, want := range map[string]fs.FileMode{"a": 0o711 | fs.ModeDir, filepath.Join("a", "b"): 0o777 | fs.ModeDir, filepath.Join("a", "b", "c"): 0o777 | fs.ModeDir} {
		if got := modeAt(t, filepath.Join(dir, d)); got != want {
			t.Errorf("%s is %v, and want %v", d, got, want)
		}
	}
	shared := filepath.Join(dir, "shared")
	if err := os.Mkdir(shared, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o770|fs.ModeSetgid); err != nil {
		t.Fatal(err)
	}
	r, err = Mkdir(root, filepath.Join("shared", "new"), 0o750)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	if err := os.Mkdir(filepath.Join(shared, "ref"), 0o700); err != nil {
		t.Fatal(err)
	}
	took := modeAt(t, filepath.Join(shared, "ref"))&fs.ModeSetgid != 0 // what a folder made here takes
	want := 0o750 | fs.ModeDir
	if took {
		want |= fs.ModeSetgid
	}
	if got := modeAt(t, filepath.Join(shared, "new")); got != want {
		t.Errorf("a folder made in a setgid folder is %v, and want %v", got, want)
	}
	if !took {
		t.Log("a folder made in a setgid folder takes no setgid bit on this system, so only the mode is checked")
	}
}

// A pipe at a component is refused at once with ErrOutside, never opened to wait for a writer; a
// folder this call made that is another account's when opened is refused with ErrOutside, and
// nothing is done to it.
//
// What it costs when this fails: a pipe a second account put at a name hangs the program, or, run
// as root, a folder another account swapped in is given the mode and the files meant for this one.
func TestMkdirRefusesAPipeAtOnceAndAFolderNotThisAccounts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := rootAt(t, dir)
	done := make(chan error, 1)
	go func() {
		r, err := Mkdir(root, filepath.Join("pipe", "x"), 0o700)
		if err == nil {
			r.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrOutside) {
			t.Errorf("Mkdir through a pipe: %v, and want ErrOutside", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("Mkdir through a pipe is still waiting after ten seconds")
		if w, err := os.OpenFile(filepath.Join(dir, "pipe"), os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			defer w.Close()
		}
		<-done
	}
	if err := os.Mkdir(filepath.Join(dir, "made"), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir, "made"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	held, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh(f, held, "made", os.Geteuid()+1); !errors.Is(err, ErrOutside) {
		t.Errorf("a made folder of another account: %v, and want ErrOutside", err)
	}
	if err := fresh(f, held, "made", os.Geteuid()); err != nil {
		t.Errorf("a made folder of this account's, empty: %v, and want nil", err)
	}
}

// The one swap Mkdir does not refuse, said in its doc: an empty folder of this account, renamed
// in at the name before the made folder's mode is set, is taken as the one made and given perm.
// This test holds what the doc says; when Mkdir comes
// to refuse it, the test and the doc change together.
//
// What it costs when this fails unnoticed: the doc says less than Mkdir does, or more.
func TestMkdirTakesAnEmptyFolderOfThisAccountRenamedInAsMade(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	private := filepath.Join(dir, "private")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(private, 0o700); err != nil {
		t.Fatal(err)
	}
	root := rootAt(t, dir)
	made := filepath.Join(dir, "new")
	done := false
	r, err := mkdir(root, "new", 0o755, func(step string) {
		if step != stepMadeFolder || done {
			return
		}
		done = true
		if err := os.Remove(made); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(private, made); err != nil {
			t.Fatal(err)
		}
	}, false)
	if err == nil {
		r.Close()
	}
	if !done || err != nil {
		t.Errorf("an empty folder of this account renamed in (reached %v): %v, and want it taken as made, as the doc says", done, err)
	}
	if got := modeAt(t, made); got != 0o755|fs.ModeDir {
		t.Errorf("the folder renamed in is %v, and want %v, perm, as the doc says", got, 0o755|fs.ModeDir)
	}
}

// A folder swapped between its open and its open as a root, for a folder renamed from beside it,
// is refused with ErrOutside, and nothing is made in the one swapped in.
//
// What it costs when this fails: the answered root is a folder other than the one checked, and
// the caller's files land in it.
func TestMkdirRefusesAFolderSwappedBeforeItIsOpenedAsARoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "beside"), 0o700); err != nil {
		t.Fatal(err)
	}
	root := rootAt(t, dir)
	made := filepath.Join(dir, "new")
	done := false
	r, err := mkdir(root, filepath.Join("new", "deeper"), 0o700, func(step string) {
		if step != stepOpenedFolder || done {
			return
		}
		done = true
		if err := os.Remove(made); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(dir, "beside"), made); err != nil {
			t.Fatal(err)
		}
	}, false)
	if err == nil {
		r.Close()
	}
	if !done || !errors.Is(err, ErrOutside) {
		t.Errorf("a folder swapped before its open as a root (reached %v): %v, and want ErrOutside", done, err)
	}
	if names := namesIn(made); len(names) != 0 {
		t.Errorf("the folder swapped in holds %q, and want nothing made in it", names)
	}
}

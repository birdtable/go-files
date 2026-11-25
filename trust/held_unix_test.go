//go:build unix

package trust

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// hold opens name in the folder d through a root, as a program holds what it judges.
func hold(t *testing.T, d, name string) *os.File {
	t.Helper()
	root, err := os.OpenRoot(d)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f, err := root.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// What it costs when this fails: on a real disk a folder or file held open that another account
// may write passes, or one only this account may change is refused, or the fix a refusal names
// does not make the held file pass; a closed file is a pass.
func TestHeldOnThisDiskJudgesTheFolderAndFileItHolds(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	if err := os.Chmod(d, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(d, "f")
	mk(t, p, File, 0o600)
	folder, file := hold(t, d, "."), hold(t, d, "f")
	for _, k := range []Kind{Folder, PrivateFolder} {
		if err := Held(folder, k, Who{}); err != nil {
			t.Errorf("the test's own folder as kind %d: %v", k, err)
		}
	}
	for _, k := range []Kind{File, Key} {
		if err := Held(file, k, Who{}); err != nil {
			t.Errorf("a file of mode 0600 as kind %d: %v", k, err)
		}
	}
	wantRefused(t, "a folder held as a file", Held(folder, File, Who{}), refusal{want: ErrNotPlain, says: "a folder"})
	wantRefused(t, "this account's file, root alone trusted", Held(file, File, Who{RootOnly: true}), refusal{want: ErrNotOwners, says: "Path gives the name's"})
	if err := os.Chmod(p, 0o666); err != nil {
		t.Fatal(err)
	}
	wantRefused(t, "a held file others may write", Held(file, File, Who{}), refusal{want: ErrOthersWrite, says: "Path gives the name's"})
	_, err := Path(p, File, Who{})
	u, ok := errors.AsType[*Unsafe](err)
	if !ok || !errors.Is(err, ErrOthersWrite) {
		t.Fatalf("Path of the held file's name: %v, and want ErrOthersWrite", err)
	}
	applyFix(t, u.Fix, p)
	if err := Held(file, File, Who{}); err != nil {
		t.Errorf("after Path's %+v: %v", u.Fix, err)
	}
	file.Close()
	if err := Held(file, File, Who{}); err == nil {
		t.Error("a closed file: a pass, and want an error")
	}
}

// What it costs when this fails: Held judges the file now at the held file's name, not the file
// held, so a program that holds a file others may write passes it once a file of its own is
// renamed over the name; or its refusal carries a Fix for the name, whose commands change a file,
// or a link, Held never read; or on macOS, whose list is read by the name, a held file whose name
// leads nowhere is said not to exist, which a caller reads as no file, though it holds one. Off
// macOS the held file is judged, with no Fix; on macOS a name that leads to another file, or to
// none, at each try is refused with ErrReplaced.
func TestHeldJudgesTheFileHeldNotTheOneNowAtItsName(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	p, q := filepath.Join(d, "f"), filepath.Join(d, "q")
	mk(t, p, File, 0o666)
	renamed := hold(t, d, "f")
	mk(t, q, File, 0o600)
	if err := os.Rename(q, p); err != nil {
		t.Fatal(err)
	}
	m := filepath.Join(d, "m")
	mk(t, m, File, 0o666)
	moved := hold(t, d, "m")
	if err := os.Rename(m, filepath.Join(d, "m2")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(d, "target")
	mk(t, target, File, 0o666)
	if err := os.Symlink(target, filepath.Join(d, "link")); err != nil {
		t.Fatal(err)
	}
	linked, err := os.Open(filepath.Join(d, "link"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { linked.Close() })
	// A descriptor of the held file under a relative name, as one opened before a chdir: the test's
	// own folder holds nothing by that name.
	fd, err := syscall.Dup(int(renamed.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	relative := os.NewFile(uintptr(fd), "held-by-a-relative-name")
	t.Cleanup(func() { relative.Close() })
	for _, c := range []struct {
		name string
		f    *os.File
	}{
		{"a name another file was renamed over", renamed},
		{"a name moved away, nothing at it", moved},
		{"a name opened through a link at its last element", linked},
		{"a relative name, after a chdir", relative},
	} {
		err := Held(c.f, File, Who{})
		if errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: %v, and want no not-exist for a file held", c.name, err)
		}
		if listByName {
			wantRefused(t, c.name, err, refusal{want: ErrReplaced, says: c.f.Name()})
		} else {
			wantRefused(t, c.name, err, refusal{want: ErrOthersWrite, says: "Path gives the name's"})
		}
	}
}

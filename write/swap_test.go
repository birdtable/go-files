package write

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test runs a copy of this test binary as the program Swap replaces; the module has no runner
	"path/filepath"
	"strings"
	"testing"
)

// What it costs when this fails: an update leaves the program's name with neither the old program
// nor the new one, or writes over a file the caller kept aside.
func TestSwapPutsTheNewFileInPlaceAndTheOldOneAside(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file, fresh, aside := filepath.Join(dir, "app"), filepath.Join(dir, "app.new"), filepath.Join(dir, "app.old")
	put0(t, file, "old")
	put0(t, fresh, "new")
	if err := Swap(file, fresh, aside, IfHolds(sha256.Sum256([]byte("old")))); err != nil {
		t.Fatal(err)
	}
	holds(t, file, "new")
	holds(t, aside, "old")
	alone(t, dir, "app", "app.old")
}

// What it costs when this fails: on Windows an update cannot replace the program that runs it, or
// leaves it half moved; on Unix the swap of a running program differs from the swap of any file.
func TestSwapReplacesAProgramWhileItRuns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file, fresh, aside := filepath.Join(dir, "app"+exeSuffix()), filepath.Join(dir, "app.new"+exeSuffix()), filepath.Join(dir, "app.old"+exeSuffix())
	old := running(t, file)
	put0(t, fresh, "new")
	if err := Swap(file, fresh, aside); err != nil {
		t.Fatal(err)
	}
	holds(t, file, "new")
	if sum := sumOf(t, aside); sum != old {
		t.Errorf("%s holds %x, and want the running program's %x", aside, sum, old)
	}
	alone(t, dir, filepath.Base(file), filepath.Base(aside))
}

// What it costs when this fails: a swap whose second move fails leaves the program's name empty,
// and the next start of the program finds nothing to run.
func TestSwapPutsTheOldFileBackWhenTheNewOneCannotTakeItsName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file, fresh, aside := filepath.Join(dir, "app"+exeSuffix()), filepath.Join(dir, "app.new"), filepath.Join(dir, "app.old")
	old := running(t, file)
	put0(t, fresh, "new")
	err := Swap(file, fresh, aside, stepWith(func(at string) {
		if at == "aside" {
			if err := os.Remove(fresh); err != nil {
				t.Error(err)
			}
		}
	}))
	if !errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrNotSwapped) {
		t.Errorf("Swap is %v, and want the second move's error matching fs.ErrNotExist, the old file put back", err)
	}
	if sum := sumOf(t, file); sum != old {
		t.Errorf("%s holds %x, and want the running program put back, %x", file, sum, old)
	}
	alone(t, dir, filepath.Base(file))
}

// What it costs when this fails: a put-back goes over a file another writer put at the name
// between the two moves, or a failed put-back is reported as the plain error a put-back answers.
func TestSwapKeepsAFilePutAtTheNameBetweenItsMoves(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file, fresh, aside := filepath.Join(dir, "app"), filepath.Join(dir, "app.new"), filepath.Join(dir, "app.old")
	put0(t, file, "old")
	put0(t, fresh, "new")
	err := Swap(file, fresh, aside, stepWith(func(at string) {
		if at == "aside" {
			put0(t, file, "theirs")
		}
	}))
	if !errors.Is(err, ErrNotSwapped) || !errors.Is(err, fs.ErrExist) {
		t.Errorf("Swap is %v, and want ErrNotSwapped matching fs.ErrExist", err)
	}
	holds(t, file, "theirs")
	holds(t, fresh, "new")
	holds(t, aside, "old")
}

// What it costs when this fails: a file put at the program's name between Swap's look and its
// first move is kept aside as if it were the program, and the caller's check of the old one
// (IfHolds) judged another file.
func TestSwapRefusesAFileSwappedInBeforeItsFirstMove(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file, fresh, aside := filepath.Join(dir, "app"), filepath.Join(dir, "app.new"), filepath.Join(dir, "app.old")
	put0(t, file, "old")
	put0(t, fresh, "new")
	err := Swap(file, fresh, aside, stepWith(func(at string) {
		if at == "swap" { // moved, not removed, so no new file can have its inode number
			if err := os.Rename(file, filepath.Join(dir, "gone")); err != nil {
				t.Error(err)
			}
			put0(t, file, "theirs")
		}
	}))
	if err == nil {
		t.Error("Swap is nil, and want the file swapped in refused")
	}
	holds(t, file, "theirs")
	holds(t, fresh, "new")
	alone(t, dir, "app", "app.new", "gone")
}

// What it costs when this fails: a file put at the new file's name after Swap looked at it takes
// the program's name, and the next start runs a program the caller never wrote.
func TestSwapRefusesAFileSwappedInBeforeItsSecondMove(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file, fresh, aside := filepath.Join(dir, "app"), filepath.Join(dir, "app.new"), filepath.Join(dir, "app.old")
	put0(t, file, "old")
	put0(t, fresh, "new")
	err := Swap(file, fresh, aside, stepWith(func(at string) {
		if at == "aside" { // moved, not removed, so no new file can have its inode number
			if err := os.Rename(fresh, filepath.Join(dir, "gone")); err != nil {
				t.Error(err)
			}
			put0(t, fresh, "theirs")
		}
	}))
	if err == nil || errors.Is(err, ErrNotSwapped) {
		t.Errorf("Swap is %v, and want the file swapped in refused, each move undone", err)
	}
	holds(t, file, "old")
	holds(t, fresh, "theirs")
	alone(t, dir, "app", "app.new", "gone")
}

// What it costs when this fails: a swap writes over a file the caller kept aside, moves a file
// that changed since the caller checked it, moves a link's target or a folder, or takes an option
// it does not apply and lets the caller think the mode or owner was set.
func TestSwapRefusesBeforeAnythingMoves(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		make func(t *testing.T, dir string) (file, fresh, aside string, opts []Option)
		want error
	}{
		{"an aside that is taken", func(t *testing.T, dir string) (string, string, string, []Option) {
			t.Helper()
			put0(t, filepath.Join(dir, "app.old"), "kept")
			return "app", "app.new", "app.old", nil
		}, fs.ErrExist},
		{"a file that changed", func(*testing.T, string) (string, string, string, []Option) {
			return "app", "app.new", "app.old", []Option{IfHolds(sha256.Sum256([]byte("other")))}
		}, ErrChanged},
		{"a Mode", func(*testing.T, string) (string, string, string, []Option) {
			return "app", "app.new", "app.old", []Option{Mode(0o700)}
		}, fs.ErrInvalid},
		{"Runnable", func(*testing.T, string) (string, string, string, []Option) {
			return "app", "app.new", "app.old", []Option{Runnable()}
		}, fs.ErrInvalid},
		{"Shared", func(*testing.T, string) (string, string, string, []Option) {
			return "app", "app.new", "app.old", []Option{Shared()}
		}, fs.ErrInvalid},
		{"an Owner", func(*testing.T, string) (string, string, string, []Option) {
			return "app", "app.new", "app.old", []Option{Owner(-1, 0)}
		}, fs.ErrInvalid},
		{"KeepMode", func(*testing.T, string) (string, string, string, []Option) {
			return "app", "app.new", "app.old", []Option{KeepMode()}
		}, fs.ErrInvalid},
		{"KeepOwner", func(*testing.T, string) (string, string, string, []Option) {
			return "app", "app.new", "app.old", []Option{KeepOwner()}
		}, fs.ErrInvalid},
		{"NoReplace", func(*testing.T, string) (string, string, string, []Option) {
			return "app", "app.new", "app.old", []Option{NoReplace()}
		}, fs.ErrInvalid},
		{"a Check", func(*testing.T, string) (string, string, string, []Option) {
			return "app", "app.new", "app.old", []Option{Check(func(*os.File) error { return nil })}
		}, fs.ErrInvalid},
		{"an aside in another folder", func(t *testing.T, dir string) (string, string, string, []Option) {
			t.Helper()
			if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
				t.Fatal(err)
			}
			return "app", "app.new", filepath.Join("sub", "app.old"), nil
		}, fs.ErrInvalid},
		{"an aside that differs from the file in case alone", func(*testing.T, string) (string, string, string, []Option) {
			return "app", "app.new", "APP", nil
		}, fs.ErrInvalid},
		{"an aside that differs from the file in normal form alone", func(t *testing.T, dir string) (string, string, string, []Option) {
			t.Helper()
			put0(t, filepath.Join(dir, "caf\u00e9"), "café")
			return "caf\u00e9", "app.new", "cafe\u0301", nil
		}, fs.ErrInvalid},
		{"an aside that is the file's ss spelled with ß", func(t *testing.T, dir string) (string, string, string, []Option) {
			t.Helper()
			put0(t, filepath.Join(dir, "pass"), "pass")
			return "pass", "app.new", "pa\u00df", nil
		}, fs.ErrInvalid},
		{"an aside ending in a dot", func(*testing.T, string) (string, string, string, []Option) {
			return "app", "app.new", "app.old.", nil
		}, fs.ErrInvalid},
		{"a fresh file that is missing", func(*testing.T, string) (string, string, string, []Option) {
			return "app", "app.none", "app.old", nil
		}, fs.ErrNotExist},
		{"a fresh file that is a folder", func(t *testing.T, dir string) (string, string, string, []Option) {
			t.Helper()
			if err := os.Mkdir(filepath.Join(dir, "app.dir"), 0o700); err != nil {
				t.Fatal(err)
			}
			return "app", "app.dir", "app.old", nil
		}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			put0(t, filepath.Join(dir, "app"), "old")
			put0(t, filepath.Join(dir, "app.new"), "new")
			file, fresh, aside, opts := c.make(t, dir)
			before := namesIn(t, dir)
			err := Swap(filepath.Join(dir, file), filepath.Join(dir, fresh), filepath.Join(dir, aside), opts...)
			if err == nil || (c.want != nil && !errors.Is(err, c.want)) {
				t.Errorf("Swap is %v, and want it refused matching %v", err, c.want)
			}
			holds(t, filepath.Join(dir, "app"), "old")
			holds(t, filepath.Join(dir, "app.new"), "new")
			if errors.Is(c.want, fs.ErrExist) {
				holds(t, filepath.Join(dir, "app.old"), "kept")
			}
			alone(t, dir, before...)
		})
	}
}

// What it costs when this fails: a link at the program's name is followed, and Swap moves the
// name of a link aside as if it were the program.
func TestSwapRefusesALinkAtTheName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	put0(t, filepath.Join(dir, "target"), "old")
	put0(t, filepath.Join(dir, "app.new"), "new")
	if err := os.Symlink("target", filepath.Join(dir, "app")); err != nil {
		t.Fatal(err) // Windows makes one without elevation, as developer mode is on there (plantLink)
	}
	if err := Swap(filepath.Join(dir, "app"), filepath.Join(dir, "app.new"), filepath.Join(dir, "app.old")); err == nil {
		t.Error("Swap is nil, and want the link refused")
	}
	alone(t, dir, "app", "app.new", "target")
}

// What it costs when this fails: a folder that could not be flushed is reported as a swap that is
// on the disk, and a power cut brings back the old program unannounced.
func TestSwapSaysAFolderItCouldNotFlush(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file, fresh, aside := filepath.Join(dir, "app"), filepath.Join(dir, "app.new"), filepath.Join(dir, "app.old")
	put0(t, file, "old")
	put0(t, fresh, "new")
	err := Swap(file, fresh, aside, syncWith(func(*os.Root, string) error { return errors.New("no flush") }))
	if !errors.Is(err, ErrNotDurable) {
		t.Errorf("Swap is %v, and want ErrNotDurable", err)
	}
	holds(t, file, "new")
	holds(t, aside, "old")
}

// running copies this test binary to path, starts it as the helper "run", which waits for its
// stdin to end, and answers the copy's SHA-256. The test's end closes its stdin and waits for it.
func running(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := File(path, b, Runnable()); err != nil {
		t.Fatal(err)
	}
	// not ended when the test's context is: the cleanup ends it by its stdin and judges its exit
	cmd := exec.CommandContext(context.WithoutCancel(t.Context()), path, helperArg, "run")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = in.Close()
		if err := cmd.Wait(); err != nil {
			t.Errorf("the program swapped while it ran: %v", err)
		}
	})
	if line, err := bufio.NewReader(out).ReadString('\n'); err != nil || line != "running\n" {
		t.Fatalf("the program says %q (%v), and want it running", line, err)
	}
	return sha256.Sum256(b)
}

func sumOf(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatal(err)
	}
	return [sha256.Size]byte(h.Sum(nil))
}

func namesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// exeSuffix is .exe where this test binary's name ends in it, as on Windows, which runs no file
// without it, and "" elsewhere.
func exeSuffix() string {
	exe, err := os.Executable()
	if err != nil || !strings.EqualFold(filepath.Ext(exe), ".exe") {
		return ""
	}
	return ".exe"
}

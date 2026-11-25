package confine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// writeMode writes a file at p with perm, which Windows does not read.
func writeMode(t *testing.T, p string, perm fs.FileMode) {
	t.Helper()
	if err := os.WriteFile(p, []byte("data"), perm); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, perm); err != nil { // the umask narrows WriteFile's
		t.Fatal(err)
	}
}

// What it costs when this fails: a program asks whether a file in its store would run and is
// answered through a link the store holds, to a file outside it or inside it, or is told a note runs,
// or a missing file or a link is answered a plain false where the answer could not be had.
func TestIsRunnableAnswersTheFileItselfAndRefusesALinkAtAnyComponent(t *testing.T) {
	t.Parallel()
	in, out := t.TempDir(), t.TempDir()
	writeMode(t, filepath.Join(out, "tool.exe"), 0o755)
	writeMode(t, filepath.Join(in, "tool.exe"), 0o755)
	writeMode(t, filepath.Join(in, "notes.md"), 0o644)
	if err := os.Mkdir(filepath.Join(in, "dir.exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	plantLink(t, filepath.Join(out, "tool.exe"), filepath.Join(in, "run.exe"))
	plantLink(t, out, filepath.Join(in, "sub"))
	if err := os.Mkdir(filepath.Join(in, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMode(t, filepath.Join(in, "real", "tool.exe"), 0o755)
	plantLink(t, "tool.exe", filepath.Join(in, "near.exe")) // relative, so it stays inside the root
	plantLink(t, "real", filepath.Join(in, "near"))
	root := openRoot(t, in)
	for _, row := range []struct {
		name    string
		want    bool
		outside bool
		gone    bool
	}{
		{"tool.exe", true, false, false},
		{"notes.md", false, false, false},
		{"dir.exe", false, false, false},
		{"gone.exe", false, false, true},
		{"run.exe", false, true, false},
		{filepath.Join("sub", "tool.exe"), false, true, false},
		{"near.exe", false, true, false},
		{filepath.Join("near", "tool.exe"), false, true, false},
		{filepath.Join("..", filepath.Base(out), "tool.exe"), false, true, false},
	} {
		got, err := confine.IsRunnable(root, row.name)
		_, isPath := errors.AsType[*fs.PathError](err)
		switch {
		case got != row.want:
			t.Errorf("%s: %v, %v, and want %v", row.name, got, err, row.want)
		case row.outside != errors.Is(err, confine.ErrOutside) || row.outside && !isPath:
			t.Errorf("%s: %v, and want an *fs.PathError matching ErrOutside: %v", row.name, err, row.outside)
		case row.gone != errors.Is(err, fs.ErrNotExist), !row.outside && !row.gone && err != nil:
			t.Errorf("%s: %v, and want fs.ErrNotExist: %v", row.name, err, row.gone)
		}
	}
}

// What it costs when this fails: a process that swaps a file of the store for a link to a program
// outside it, between a look and the open, has that program answered runnable as the store's.
func TestIsRunnableReadsNothingOutsideWhenTheNameIsSwappedForALinkBeforeTheOpen(t *testing.T) {
	t.Parallel()
	in, out := t.TempDir(), t.TempDir()
	writeMode(t, filepath.Join(out, "tool.exe"), 0o755)
	writeMode(t, filepath.Join(in, "x"), 0o644)
	root := openRoot(t, in)
	swapped := false
	got, err := confine.IsRunnableBeforeOpen(root, "x", func() {
		if err := os.Remove(filepath.Join(in, "x")); err != nil {
			t.Fatal(err)
		}
		plantLink(t, filepath.Join(out, "tool.exe"), filepath.Join(in, "x"))
		swapped = true
	})
	if !swapped || got || !errors.Is(err, confine.ErrOutside) {
		t.Errorf("x swapped (%v) for a link to %s before the open: %v, %v, and want false and ErrOutside", swapped, filepath.Join(out, "tool.exe"), got, err)
	}
}

// failingCloser is a handle whose close fails.
type failingCloser struct{}

func (failingCloser) Close() error { return errClose }

var errClose = errors.New("the close failed")

// What it costs when this fails: a handle whose close fails after the answer is had leaves a file
// answered runnable with an error beside it, where every other failure answers false.
func TestIsRunnableAnswersFalseWhenTheCloseFails(t *testing.T) {
	t.Parallel()
	got, err := confine.Closing(failingCloser{}, true, nil)
	if got || !errors.Is(err, errClose) {
		t.Errorf("a runnable file whose close fails: %v, %v, and want false and the close's error", got, err)
	}
}

// What it costs when this fails: a program that opened a file itself, as git's reader opens one, is
// answered otherwise than IsRunnable answers the same file, or a folder it holds is answered runnable.
func TestIsRunnableFileAnswersAFileTheCallerOpenedForWhateverFileItIs(t *testing.T) {
	t.Parallel()
	in := t.TempDir()
	writeMode(t, filepath.Join(in, "tool.exe"), 0o755)
	writeMode(t, filepath.Join(in, "notes.md"), 0o644)
	plantLink(t, filepath.Join(in, "tool.exe"), filepath.Join(in, "run.exe"))
	for _, row := range []struct {
		name string
		want bool
	}{
		{"tool.exe", true},
		{"notes.md", false},
		{"run.exe", true},
		{".", false},
	} {
		f, err := os.Open(filepath.Join(in, row.name))
		if err != nil {
			t.Fatal(err)
		}
		got, err := confine.IsRunnableFile(f)
		if got != row.want || err != nil {
			t.Errorf("%s opened by the test: %v, %v, and want %v and no error", row.name, got, err, row.want)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

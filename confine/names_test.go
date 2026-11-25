package confine_test

import (
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// namesBoth is the count of name beneath root by Names and by NamesFile on a handle the test
// opened, which must agree.
func namesBoth(t *testing.T, root *os.Root, name string) int {
	t.Helper()
	n, err := confine.Names(root, name)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	f, err := root.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	byHandle, err := confine.NamesFile(f)
	if err != nil {
		t.Fatalf("%s by the handle: %v", name, err)
	}
	if byHandle != n {
		t.Errorf("%s: %d names by Names and %d by NamesFile", name, n, byHandle)
	}
	return n
}

// What it costs when this fails: a guard that refuses a file with a second name, which can be a
// file outside the root, is told one name for a file with two, or two for a file with one, and
// writes through to the other file or refuses a plain one.
func TestNamesCountsAHardLinkMadeAndRemovedByEitherNameAndByTheHandle(t *testing.T) {
	t.Parallel()
	in := t.TempDir()
	writeMode(t, filepath.Join(in, "a"), 0o644)
	root := openRoot(t, in)
	if n := namesBoth(t, root, "a"); n != 1 {
		t.Errorf("a of one name: %d, and want 1", n)
	}
	if err := os.Link(filepath.Join(in, "a"), filepath.Join(in, "b")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if n := namesBoth(t, root, name); n != 2 {
			t.Errorf("%s, after a hard link: %d, and want 2", name, n)
		}
	}
	held, err := root.Open("a")
	if err != nil {
		t.Fatal(err)
	}
	n, err := confine.NamesFile(held)
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	if n != 2 || err != nil {
		t.Errorf("a by a handle held over the link: %d, %v, and want 2", n, err)
	}
	if err := os.Remove(filepath.Join(in, "a")); err != nil { // handles closed first, for Windows
		t.Fatal(err)
	}
	if n := namesBoth(t, root, "b"); n != 1 {
		t.Errorf("b, after a's removal: %d, and want 1", n)
	}
}

// What it costs when this fails: a folder's count, which on Unix counts its subfolders, is
// answered as a file's, so a guard reads a folder of two subfolders as a file with a second name,
// or one of none as a plain file.
func TestNamesRefusesAFolderByEitherForm(t *testing.T) {
	t.Parallel()
	in := t.TempDir()
	if err := os.Mkdir(filepath.Join(in, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, in)
	for _, name := range []string{"dir", "."} {
		n, err := confine.Names(root, name)
		if _, isPath := errors.AsType[*fs.PathError](err); n != 0 || !errors.Is(err, fs.ErrInvalid) || !isPath {
			t.Errorf("%s: %d, %v, and want 0 and an *fs.PathError matching fs.ErrInvalid", name, n, err)
		}
	}
	f, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if n, err := confine.NamesFile(f); n != 0 || !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("a folder the test opened: %d, %v, and want 0 and fs.ErrInvalid", n, err)
	}
}

// What it costs when this fails: a program asks how many names a file of its store has and is
// answered through a link the store holds, to a file outside it or inside it, so the count is
// another file's, or a missing file is answered a count.
func TestNamesRefusesALinkAtAnyComponentInsideTheRootOrOut(t *testing.T) {
	t.Parallel()
	in, out := t.TempDir(), t.TempDir()
	writeMode(t, filepath.Join(out, "a"), 0o644)
	writeMode(t, filepath.Join(in, "a"), 0o644)
	if err := os.Mkdir(filepath.Join(in, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMode(t, filepath.Join(in, "real", "a"), 0o644)
	plantLink(t, filepath.Join(out, "a"), filepath.Join(in, "far"))
	plantLink(t, out, filepath.Join(in, "sub"))
	plantLink(t, "a", filepath.Join(in, "near")) // relative, so it stays inside the root
	plantLink(t, "real", filepath.Join(in, "nearDir"))
	root := openRoot(t, in)
	for _, row := range []struct {
		name          string
		outside, gone bool
	}{
		{"far", true, false},
		{filepath.Join("sub", "a"), true, false},
		{"near", true, false},
		{filepath.Join("nearDir", "a"), true, false},
		{filepath.Join("..", filepath.Base(out), "a"), true, false},
		{"gone", false, true},
	} {
		n, err := confine.Names(root, row.name)
		_, isPath := errors.AsType[*fs.PathError](err)
		switch {
		case n != 0:
			t.Errorf("%s: %d, %v, and want 0", row.name, n, err)
		case row.outside && (!errors.Is(err, confine.ErrOutside) || !isPath):
			t.Errorf("%s: %v, and want an *fs.PathError matching ErrOutside", row.name, err)
		case row.gone && !errors.Is(err, fs.ErrNotExist):
			t.Errorf("%s: %v, and want fs.ErrNotExist", row.name, err)
		}
	}
	for _, name := range []string{"a", filepath.Join("real", "a")} {
		if n := namesBoth(t, root, name); n != 1 {
			t.Errorf("%s, the file a link leads to: %d, and want 1", name, n)
		}
	}
}

// What it costs when this fails: a process that swaps a file of the store for a link to a file
// outside it, between a look and the open, has that file's count answered as the store's.
func TestNamesReadsNothingOutsideWhenTheNameIsSwappedForALinkBeforeTheOpen(t *testing.T) {
	t.Parallel()
	in, out := t.TempDir(), t.TempDir()
	writeMode(t, filepath.Join(out, "a"), 0o644)
	if err := os.Link(filepath.Join(out, "a"), filepath.Join(out, "b")); err != nil {
		t.Fatal(err)
	}
	writeMode(t, filepath.Join(in, "x"), 0o644)
	root := openRoot(t, in)
	swapped := false
	n, err := confine.NamesBeforeOpen(root, "x", func() {
		if err := os.Remove(filepath.Join(in, "x")); err != nil {
			t.Fatal(err)
		}
		plantLink(t, filepath.Join(out, "a"), filepath.Join(in, "x"))
		swapped = true
	})
	if !swapped || n != 0 || !errors.Is(err, confine.ErrOutside) {
		t.Errorf("x swapped (%v) for a link to %s, of two names, before the open: %d, %v, and want 0 and ErrOutside", swapped, filepath.Join(out, "a"), n, err)
	}
}

// What it costs when this fails: a handle whose close fails after the count is had leaves a count
// answered with an error beside it, where every other failure answers 0.
func TestNamesAnswersZeroWhenTheCloseFails(t *testing.T) {
	t.Parallel()
	n, err := confine.ClosingCount(failingCloser{}, 2, nil)
	if n != 0 || !errors.Is(err, errClose) {
		t.Errorf("a count whose close fails: %d, %v, and want 0 and the close's error", n, err)
	}
}

// What it costs when this fails: a volume that reports 2^31 names or more, a server's reply, wraps
// on a 32-bit build to a count below 2, and Open and treesum pass a file with a second name, or the
// refusal is no longer the hard link's.
func TestNamesCountsWhatTheVolumeReportsAndAnswersACountPastAnIntAsTheMost(t *testing.T) {
	t.Parallel()
	for _, n := range []uint64{0, 1, 2, math.MaxInt32} {
		if got := confine.CountOf(n); got != int(n) {
			t.Errorf("a count of %d: %d, and want %d", n, got, n)
		}
	}
	for _, n := range []uint64{1 << 31, math.MaxUint32, math.MaxUint64} {
		if got := confine.CountOf(n); got != math.MaxInt32 {
			t.Errorf("a count of %d: %d, and want %d, a count over 1, which Open and the hash refuse", n, got, math.MaxInt32)
		}
	}
}

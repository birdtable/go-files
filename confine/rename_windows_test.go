package confine_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// swaps is false: Windows has no call that swaps two names in one step.
const swaps = false

// RenameExchange on Windows refuses with ErrNoExchange, which matches errors.ErrUnsupported and is
// not ErrOutside, and moves nothing.
//
// What it costs when this fails: a swap on Windows is done as two moves, and a crash or another
// process between them leaves one name gone, or the refusal reads as a path that left the folder.
func TestOnWindowsRenameExchangeRefusesAndMovesNothing(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	err := confine.RenameExchange(root, "beside.txt", filepath.Join("real", "HEAD"))
	if !errors.Is(err, confine.ErrNoExchange) || !errors.Is(err, errors.ErrUnsupported) || errors.Is(err, confine.ErrOutside) {
		t.Errorf("RenameExchange: %v, want ErrNoExchange and errors.ErrUnsupported", err)
	}
	unchanged(t, dir)
	if _, err := os.Lstat(filepath.Join(dir, "moved")); err == nil {
		t.Error("moved is there")
	}
}

// On Windows RenameNoReplace refuses, with an error matching ErrOutside and before any call, a
// name with a component that ends in a dot or a space, from or to, and makes nothing. The Win32
// name rules, which os.Root's opens take, strip both, so x. is x to os.Root; the NT calls of the
// move take x. as written.
//
// What it costs when this fails: one name means two files, so a look through os.Root finds x while
// the move makes x. beside it, a file no Win32 program opens by its name.
func TestOnWindowsANameThatEndsInADotOrASpaceIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tmp", "x"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	cases := []struct{ what, from, to string }{
		{"onto x. beside x", "tmp", "x."},
		{"onto x and a space beside x", "tmp", "x "},
		{"from tmp.", "tmp.", "y"},
		{"into sub.", "tmp", filepath.Join("sub.", "y")},
	}
	for _, c := range cases {
		if err := confine.RenameNoReplace(root, c.from, c.to); !errors.Is(err, confine.ErrOutside) {
			t.Errorf("RenameNoReplace %s: %v, want ErrOutside", c.what, err)
		}
	}
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range left {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"sub", "tmp", "x"}) {
		t.Errorf("the folder holds %q, want sub, tmp and x, and nothing named x.", names)
	}
	for _, name := range []string{"tmp", "x"} {
		if got, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(got) != name+"\n" {
			t.Errorf("%s holds %q, %v", name, got, err)
		}
	}
	if sub, err := os.ReadDir(filepath.Join(dir, "sub")); err != nil || len(sub) != 0 {
		t.Errorf("sub holds %v, %v", sub, err)
	}
}

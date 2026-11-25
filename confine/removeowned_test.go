package confine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// readOnly takes the write bit from each name in dir, a folder or a file, as the go command leaves
// a module cache (on Windows the read-only attribute), and fails the test unless the bit is gone. At
// the test's end every folder and file left in dir has the bit again, so t.TempDir can remove it.
func readOnly(t *testing.T, dir string, names ...string) {
	t.Helper()
	t.Cleanup(func() { writableAgain(t, dir) })
	for _, name := range names {
		path := filepath.Join(dir, filepath.FromSlash(name))
		mode := fs.FileMode(0o444)
		if fi, err := os.Lstat(path); err == nil && fi.IsDir() {
			mode = 0o555
		}
		must(t, os.Chmod(path, mode))
		if writable(t, dir, name) {
			t.Fatalf("%s still has the write bit after a chmod to %v", name, mode)
		}
	}
}

// writableAgain gives every folder and file in dir its owner's write bit, following no link.
func writableAgain(t *testing.T, dir string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.Type()&fs.ModeSymlink != 0 {
			return err
		}
		mode := fs.FileMode(0o644)
		if d.IsDir() {
			mode = 0o755
		}
		return os.Chmod(path, mode)
	})
	if err != nil {
		t.Error(err)
	}
}

// writable is whether name in dir, followed by no link, has its owner's write bit.
func writable(t *testing.T, dir, name string) bool {
	t.Helper()
	fi, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(name)))
	must(t, err)
	return fi.Mode()&0o200 != 0
}

// RemoveAllOwned removes a tree whose folders and files its owner made read-only, three deep, as
// the go command leaves a module cache: folders 0o555 and files 0o444. An empty read-only folder is
// removed too.
//
// What it costs when this fails: a caller that gives a program a home of its own for one run cannot
// remove it after the run, and the homes pile up.
func TestRemoveAllOwnedRemovesATreeItsOwnerMadeReadOnly(t *testing.T) {
	t.Parallel()
	root, dir := removeFolder(t)
	readOnly(t, dir, "tree/a.txt", "tree/sub/b.txt", "tree/sub/deeper/c.txt", "tree/sub/deeper", "tree/sub", "tree", "empty")
	for _, name := range []string{"tree", "empty"} {
		if err := RemoveAllOwned(root, name); err != nil {
			t.Errorf("RemoveAllOwned(%s): %v", name, err)
		}
		gone(t, dir, name)
	}
	holds(t, dir, filepath.Join("real", "HEAD"), "head")
}

// RemoveAllOwned follows no link and changes none: real, a read-only folder that a link in the tree
// and the link at the name lead to, keeps its mode and its file once both are removed.
//
// What it costs when this fails: a link planted in a run's home has the removal give the write bit
// to a folder outside it, the caller's own read-only state or another account's, which it was
// never asked to touch.
func TestRemoveAllOwnedChangesNothingALinkLeadsTo(t *testing.T) {
	t.Parallel()
	root, dir := removeFolder(t)
	readOnly(t, dir, "real", "tree/sub", "tree")
	for _, name := range []string{"tree", "link"} {
		if err := RemoveAllOwned(root, name); err != nil {
			t.Errorf("RemoveAllOwned(%s): %v", name, err)
		}
		gone(t, dir, name)
	}
	if writable(t, dir, "real") {
		t.Error("real, which a link in the tree and the link at the name lead to, was given the write bit")
	}
	holds(t, dir, filepath.Join("real", "HEAD"), "head")
}

// A read-only folder swapped between RemoveAllOwned's look at it and its open is refused with
// ErrOutside before its mode is touched: the read-only folder swapped in keeps its mode and its
// file.
//
// What it costs when this fails: a process that renames a read-only folder in at the right moment
// has the removal make it writable, and every account that may enter it may then change it.
func TestRemoveAllOwnedRefusesAFolderSwappedBeforeItsModeChanges(t *testing.T) {
	t.Parallel()
	for _, at := range []string{"tree", filepath.Join("tree", "sub"), filepath.Join("tree", "sub", "deeper")} {
		root, dir := removeFolder(t)
		readOnly(t, dir, "tree/sub/deeper", "tree/sub", "tree")
		swap, swapped := swapAt(t, dir, at), false
		err := removeAll(root, "tree", func(step string) {
			swap(step)
			if _, err := os.Lstat(filepath.Join(dir, at, "keep.txt")); err == nil && !swapped {
				swapped = true
				must(t, os.Chmod(filepath.Join(dir, at), 0o555))
			}
		}, os.Geteuid())
		if !swapped || !errors.Is(err, ErrOutside) {
			t.Errorf("%s swapped before its mode changed (swapped %v): %v, and want ErrOutside", at, swapped, err)
		}
		if writable(t, dir, at) {
			t.Errorf("%s, the read-only folder swapped in, was given the write bit", at)
		}
		holds(t, dir, filepath.Join(at, "keep.txt"), "keep")
	}
}

//go:build unix

package confine

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The folder of a name, swapped between its open and its open as a root, is refused by both with
// ErrOutside, and the folder swapped in keeps its file. On Windows the folder is held open with no
// share to delete, so the rename itself is refused there.
//
// What it costs when this fails: the removal is made in a folder other than the one walked, which
// a process renamed in at the right moment.
func TestRemoveRefusesAFolderSwappedBeforeItIsOpenedAsARoot(t *testing.T) {
	t.Parallel()
	for what, call := range map[string]func(*os.Root, string, func(string)) error{"Remove": remove,
		"RemoveAll":      func(r *os.Root, n string, f func(string)) error { return removeAll(r, n, f, noOwner) },
		"RemoveAllOwned": func(r *os.Root, n string, f func(string)) error { return removeAll(r, n, f, os.Geteuid()) }} {
		root, dir := removeFolder(t)
		done := false
		err := call(root, filepath.Join("tree", "a.txt"), func(step string) {
			if step != stepOpenedFolder || done {
				return
			}
			done = true
			must(t, os.Rename(filepath.Join(dir, "tree"), filepath.Join(dir, "aside")))
			must(t, os.Rename(filepath.Join(dir, "full"), filepath.Join(dir, "tree")))
		})
		if !done || !errors.Is(err, ErrOutside) {
			t.Errorf("%s, with the folder swapped before its open as a root (reached %v): %v, and want ErrOutside", what, done, err)
		}
		holds(t, dir, filepath.Join("tree", "a.txt"), "a")
		holds(t, dir, filepath.Join("aside", "a.txt"), "a")
	}
}

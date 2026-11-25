//go:build unix

package confine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// A read-only folder another account owns keeps its mode: RemoveAllOwned removes it as RemoveAll
// does, so the system refuses the removal of what it holds with fs.ErrPermission (unless this
// account is root, which the system lets remove it). RemoveAll gives no folder the write bit.
//
// What it costs when this fails: a tree holding another account's read-only folder has the removal
// make that folder writable, a mode change only its owner may make (as root, the removal changes
// another account's folder).
func TestRemoveAllOwnedLeavesTheModeOfAFolderAnotherAccountOwns(t *testing.T) {
	t.Parallel()
	asRoot := os.Geteuid() == 0
	for what, owner := range map[string]int{"RemoveAll": noOwner, "another account's": os.Geteuid() + 1} {
		root, dir := removeFolder(t)
		readOnly(t, dir, "tree")
		err := removeAll(root, "tree", func(string) {}, owner)
		if asRoot {
			if err != nil {
				t.Errorf("%s, as root: %v, and want nil", what, err)
			}
			continue
		}
		if !errors.Is(err, fs.ErrPermission) {
			t.Errorf("%s: %v, and want fs.ErrPermission", what, err)
		}
		if writable(t, dir, "tree") {
			t.Errorf("%s gave tree the write bit", what)
		}
	}
}

// RemoveAllOwned gives a read-only folder its owner's write bit and no other: a sticky folder at
// 0o1555 holds 0o1755 while it is emptied, one at 0o500 holds 0o700, and one at 0o511 holds 0o711.
//
// What it costs when this fails: a removal that stops part way leaves a folder its group or every
// account may write, or one that has lost its sticky bit.
func TestRemoveAllOwnedGivesTheOwnerWriteBitAlone(t *testing.T) {
	t.Parallel()
	root, dir := removeFolder(t)
	t.Cleanup(func() { writableAgain(t, dir) })
	deeper := filepath.Join("tree", "sub", "deeper")
	must(t, os.Mkdir(filepath.Join(dir, deeper, "d"), 0o755))
	must(t, os.Chmod(filepath.Join(dir, deeper), 0o511))
	must(t, os.Chmod(filepath.Join(dir, "tree", "sub"), 0o500))
	must(t, os.Chmod(filepath.Join(dir, "tree"), 0o555|fs.ModeSticky))
	seen := map[string]fs.FileMode{}
	err := removeAll(root, "tree", func(step string) {
		if step != stepLooked {
			return
		}
		for _, name := range []string{"tree", filepath.Join("tree", "sub"), deeper} {
			if fi, err := os.Lstat(filepath.Join(dir, name)); err == nil {
				seen[name] = fi.Mode() & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)
			}
		}
	}, os.Geteuid())
	if err != nil {
		t.Fatalf("RemoveAllOwned(tree): %v", err)
	}
	for name, want := range map[string]fs.FileMode{"tree": 0o755 | fs.ModeSticky, filepath.Join("tree", "sub"): 0o700, deeper: 0o711} {
		if seen[name] != want {
			t.Errorf("%s held %v while it was emptied, and want %v", name, seen[name], want)
		}
	}
	gone(t, dir, "tree")
}

// The folder the name is in keeps its mode: with ro, read-only, above it, RemoveAllOwned(ro/tree)
// empties tree and is refused by the system with fs.ErrPermission at tree's removal (unless this
// account is root), and ro still lacks the write bit.
//
// What it costs when this fails: a caller removing one run's home from a folder it keeps read-only
// finds that folder made writable.
func TestRemoveAllOwnedLeavesTheFolderOfTheName(t *testing.T) {
	t.Parallel()
	root, dir := removeFolder(t)
	must(t, os.MkdirAll(filepath.Join(dir, "ro", "tree", "sub"), 0o755))
	readOnly(t, dir, "ro/tree/sub", "ro/tree", "ro")
	err := RemoveAllOwned(root, filepath.Join("ro", "tree"))
	if os.Geteuid() != 0 && !errors.Is(err, fs.ErrPermission) {
		t.Errorf("RemoveAllOwned(ro/tree): %v, and want fs.ErrPermission", err)
	}
	if writable(t, dir, "ro") {
		t.Error("ro, the folder the name is in, was given the write bit")
	}
	gone(t, dir, filepath.Join("ro", "tree", "sub"))
}

// A folder its owner left with no search bit, at 0o444, keeps its mode: RemoveAllOwned(tree) is
// refused by the system with fs.ErrPermission at it, as RemoveAll is (unless this account is
// root), and tree/sub still holds 0o444.
//
// What it costs when this fails: the removal gives the owner more than the write bit the caller
// was promised, and a folder it then cannot enter is left changed.
func TestRemoveAllOwnedLeavesAFolderWithNoSearchBit(t *testing.T) {
	t.Parallel()
	root, dir := removeFolder(t)
	t.Cleanup(func() { writableAgain(t, dir) })
	must(t, os.Chmod(filepath.Join(dir, "tree", "sub"), 0o444))
	err := RemoveAllOwned(root, "tree")
	if os.Geteuid() == 0 {
		if err != nil {
			t.Errorf("RemoveAllOwned(tree), as root: %v, and want nil", err)
		}
		return
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("RemoveAllOwned(tree): %v, and want fs.ErrPermission", err)
	}
	fi, err := os.Lstat(filepath.Join(dir, "tree", "sub"))
	must(t, err)
	if fi.Mode().Perm() != 0o444 {
		t.Errorf("tree/sub holds %v, and want -r--r--r--, the mode it had", fi.Mode().Perm())
	}
}

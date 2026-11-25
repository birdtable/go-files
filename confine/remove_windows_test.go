package confine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// A name with a component that ends in a dot or a space is refused by both with ErrOutside, and
// nothing is removed.
//
// What it costs when this fails: f.txt. is f.txt to os.Root's look and f.txt. to the removal, so
// a check sees one file and the removal takes another.
func TestOnWindowsRemoveRefusesANameThatEndsInADotOrASpace(t *testing.T) {
	t.Parallel()
	root, dir := removeFolder(t)
	for _, name := range []string{"f.txt.", "f.txt ", `tree.\a.txt`, `tree \a.txt`} {
		if err := Remove(root, name); !errors.Is(err, ErrOutside) {
			t.Errorf("Remove(%q): %v, and want ErrOutside", name, err)
		}
		if err := RemoveAll(root, name); !errors.Is(err, ErrOutside) {
			t.Errorf("RemoveAll(%q): %v, and want ErrOutside", name, err)
		}
	}
	holds(t, dir, "f.txt", "f")
	holds(t, dir, filepath.Join("tree", "a.txt"), "a")
}

// A junction before the last component is refused by both with ErrOutside; one at the name is
// removed as the junction, and one met in a tree is removed and not walked: the folder each leads
// to keeps its file.
//
// What it costs when this fails: any account plants a junction, with no privilege, in a folder a
// program clears, and the removal empties the folder the junction leads to.
func TestOnWindowsRemoveTakesAJunctionAsALink(t *testing.T) {
	t.Parallel()
	root, dir := removeFolder(t)
	plantJunction(t, filepath.Join(dir, "real"), filepath.Join(dir, "j"))
	plantJunction(t, filepath.Join(dir, "real"), filepath.Join(dir, "tree", "j"))
	for _, name := range []string{filepath.Join("j", "HEAD"), filepath.Join("tree", "j", "HEAD")} {
		if err := Remove(root, name); !errors.Is(err, ErrOutside) {
			t.Errorf("Remove(%s): %v, and want ErrOutside", name, err)
		}
		if err := RemoveAll(root, name); !errors.Is(err, ErrOutside) {
			t.Errorf("RemoveAll(%s): %v, and want ErrOutside", name, err)
		}
	}
	if err := RemoveAll(root, "tree"); err != nil {
		t.Errorf("RemoveAll(tree) with a junction in it: %v", err)
	}
	if err := Remove(root, "j"); err != nil {
		t.Errorf("Remove(j): %v", err)
	}
	gone(t, dir, "tree")
	gone(t, dir, "j")
	holds(t, dir, filepath.Join("real", "HEAD"), "head")
}

// A file with the read-only attribute is removed by both.
//
// What it costs when this fails: a program cannot clear a folder holding a file git or a tool
// made read-only, which os.RemoveAll clears.
func TestOnWindowsRemoveRemovesAReadOnlyFile(t *testing.T) {
	t.Parallel()
	root, dir := removeFolder(t)
	must(t, os.Chmod(filepath.Join(dir, "f.txt"), 0o444))
	must(t, os.Chmod(filepath.Join(dir, "tree", "sub", "b.txt"), 0o444))
	if err := Remove(root, "f.txt"); err != nil {
		t.Errorf("Remove of a read-only file: %v", err)
	}
	if err := RemoveAll(root, "tree"); err != nil {
		t.Errorf("RemoveAll of a tree holding a read-only file: %v", err)
	}
	gone(t, dir, "f.txt")
	gone(t, dir, "tree")
}

// heldOpen opens path shared for reading alone, not for delete, as a scanner holds a file it reads,
// and answers let, which closes the handle once. The test's end calls let too, and fails the test
// when the close failed: a handle closed a second time closes whatever the system has given its
// number since, another parallel test's or the runtime's, and that one then answers "The handle is
// invalid", as -race runs on Windows showed.
func heldOpen(t *testing.T, path string) (let func()) {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	var closed error
	let = func() { once.Do(func() { closed = windows.CloseHandle(h) }) }
	t.Cleanup(func() {
		let()
		if closed != nil {
			t.Errorf("the handle holding %s did not close: %v", path, closed)
		}
	})
	return let
}

// A file another process holds for a moment is a sharing violation at the removal, which is tried
// again, and the file is removed once it is let go, by Remove and in RemoveAll's tree.
//
// What it costs when this fails: on a machine with a scanner, clearing a folder fails now and
// then, and a program reports a failure it could have avoided.
func TestOnWindowsAFileHeldForAMomentIsRemoved(t *testing.T) {
	t.Parallel()
	root, dir := removeFolder(t)
	for what, name := range map[string]string{"Remove": "f.txt", "RemoveAll": filepath.Join("tree", "sub", "b.txt")} {
		time.AfterFunc(20*time.Millisecond, heldOpen(t, filepath.Join(dir, name)))
		var err error
		if what == "Remove" {
			err = Remove(root, name)
		} else {
			err = RemoveAll(root, "tree")
		}
		if err != nil {
			t.Errorf("%s with %s held for a moment: %v, and want it removed once let go", what, name, err)
		}
		gone(t, dir, name)
	}
}

// A file held past the window is refused by both with an *fs.PathError that matches
// fs.ErrPermission, and the file stays.
//
// What it costs when this fails: a caller that tells a held file from a lost one by
// fs.ErrPermission, as it does for os.Remove's denial, takes a sharing violation for another error.
func TestOnWindowsAFileHeldPastTheWindowIsRefusedAsAPermission(t *testing.T) {
	t.Parallel()
	root, dir := removeFolder(t)
	heldOpen(t, filepath.Join(dir, "f.txt"))
	heldOpen(t, filepath.Join(dir, "tree", "a.txt"))
	for what, err := range map[string]error{"Remove": Remove(root, "f.txt"), "RemoveAll": RemoveAll(root, "tree")} {
		if _, ok := errors.AsType[*fs.PathError](err); !ok || !errors.Is(err, fs.ErrPermission) {
			t.Errorf("%s of a file held past the window: %v, and want an *fs.PathError matching fs.ErrPermission", what, err)
		}
	}
	holds(t, dir, "f.txt", "f")
	holds(t, dir, filepath.Join("tree", "a.txt"), "a")
}

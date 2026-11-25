package confine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// heldFolder is a folder holding s/a.txt and an empty folder p/d, opened as a root, a folder
// outside it, and the hook renameHeld calls, which runs change once, at stepHeld.
func heldFolder(t *testing.T, change func(dir, out string)) (*os.Root, string, string, func(string)) {
	t.Helper()
	out := t.TempDir()
	root, dir, hook := raced(t, map[string]func(string){stepHeld: func(dir string) { change(dir, out) }})
	must(t, os.MkdirAll(filepath.Join(dir, "p", "d"), 0o755))
	must(t, os.Mkdir(filepath.Join(dir, "s"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "s", "a.txt"), []byte("a\n"), 0o644))
	return root, dir, out, hook
}

// A folder on the path of either name, or the source itself, cannot be moved out of the root
// between the hold and the rename, so no junction takes its place: each folder is held open with
// no share to delete. The move of s/a.txt to p/d/a.txt lands inside the root, and nothing lands
// outside it.
//
// What it costs when this fails: the rename is by handle, so a process that moves the held folder
// p/d outside the root at the right moment, and puts a junction where it was, has the file land in
// the folder it moved, outside the root, where no check of the junction sees it.
func TestOnWindowsAFolderHeldForARenameCannotBeMovedOut(t *testing.T) {
	t.Parallel()
	tried := 0
	for what, moved := range map[string]string{
		"the folder of to":         filepath.Join("p", "d"),
		"the folder above to's":    "p",
		"the folder of from":       "s",
		"the source being renamed": filepath.Join("s", "a.txt"),
	} {
		var outErr error
		root, dir, out, hook := heldFolder(t, func(dir, out string) {
			outErr = os.Rename(filepath.Join(dir, moved), filepath.Join(out, "taken"))
			if outErr == nil && moved != filepath.Join("s", "a.txt") {
				plantJunction(t, filepath.Join(out, "taken"), filepath.Join(dir, moved))
			}
		})
		err := renameHeld(root, filepath.Join("s", "a.txt"), filepath.Join("p", "d", "a.txt"), hook)
		if outErr == nil {
			t.Errorf("%s was moved out of the root while it was held", what)
		}
		if err != nil {
			t.Errorf("the rename, with %s tried: %v", what, err)
		}
		holds(t, dir, filepath.Join("p", "d", "a.txt"), "a\n")
		if left, err := os.ReadDir(out); err != nil || len(left) != 0 {
			t.Errorf("with %s tried, outside the root holds %v, %v", what, left, err)
		}
		tried++
	}
	if tried != 4 {
		t.Fatalf("tried %d of 4", tried)
	}
}

// A name made at to between the hold and the rename is refused with fs.ErrExist by the rename
// itself, and both files keep their bytes: the rename asks the system not to replace.
//
// What it costs when this fails: a file another process made at to after the look is replaced, and
// lost, as a plain rename would.
func TestOnWindowsANameMadeAtToBeforeTheRenameIsNotReplaced(t *testing.T) {
	t.Parallel()
	root, dir, _, hook := heldFolder(t, func(dir, _ string) {
		must(t, os.WriteFile(filepath.Join(dir, "p", "d", "a.txt"), []byte("other\n"), 0o644))
	})
	err := renameHeld(root, filepath.Join("s", "a.txt"), filepath.Join("p", "d", "a.txt"), hook)
	if !errors.Is(err, fs.ErrExist) || errors.Is(err, ErrOutside) {
		t.Errorf("the rename onto a name made since the look: %v, want fs.ErrExist", err)
	}
	holds(t, dir, filepath.Join("p", "d", "a.txt"), "other\n")
	holds(t, dir, filepath.Join("s", "a.txt"), "a\n")
}

// The held folder of to, p/d, made a junction to a folder outside the root in place, between the
// hold and the rename, does not take the file outside: the rename is refused, or the file lands in
// p/d itself, and the folder outside stays empty. The folder is not moved, so its hold does not
// stop this: a handle opened with FILE_WRITE_ATTRIBUTES, an access no share mode checks, or with
// FILE_WRITE_DATA, which the hold shares, may set a mount point on an empty folder. The move's
// name is parsed from the held folder's handle, so it starts at that folder, and the test shows
// whether its reparse point is read again there.
//
// What it costs when this fails: a process that may write the attributes of an empty folder in
// the root has a rename into it land wherever the junction it sets leads, outside the root, with
// no reparse point seen at the hold.
func TestOnWindowsTheHeldFolderOfToMadeAJunctionInPlaceTakesNothingOutside(t *testing.T) {
	t.Parallel()
	tried := 0
	for what, access := range map[string]uint32{
		"FILE_WRITE_ATTRIBUTES": windows.FILE_WRITE_ATTRIBUTES,
		"FILE_WRITE_DATA":       windows.FILE_WRITE_DATA,
	} {
		var planted windows.Handle
		root, dir, out, hook := heldFolder(t, func(dir, out string) {
			name, err := windows.UTF16PtrFromString(filepath.Join(dir, "p", "d"))
			must(t, err)
			planted, err = windows.CreateFile(name, access,
				windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
				windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
			if err != nil {
				t.Fatalf("with %s, p/d could not be opened while it was held, so no junction was tried: %v", what, err)
			}
			if err := setMountPoint(t, planted, out); err != nil {
				t.Fatalf("with %s, p/d could not be made a junction while it was held, so the rename was not tried on one: %v", what, err)
			}
		})
		err := renameHeld(root, filepath.Join("s", "a.txt"), filepath.Join("p", "d", "a.txt"), hook)
		if left, rerr := os.ReadDir(out); rerr != nil || len(left) != 0 {
			t.Errorf("with %s, outside the root holds %v, %v, after the rename answered %v", what, left, rerr, err)
		}
		clearMountPoint(t, planted)
		must(t, windows.CloseHandle(planted))
		if err != nil {
			t.Logf("with %s, the rename was refused: %v", what, err)
			holds(t, dir, filepath.Join("s", "a.txt"), "a\n")
		} else {
			t.Logf("with %s, the rename landed in the held folder", what)
			holds(t, dir, filepath.Join("p", "d", "a.txt"), "a\n")
		}
		tried++
	}
	if tried != 2 {
		t.Fatalf("tried %d of 2", tried)
	}
}

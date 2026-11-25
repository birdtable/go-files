//go:build unix

package lock

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// A name replaced after the check, between the open and the lock, is refused below a root as by
// a path: the name is read again through the root once the lock is held.
//
// On Windows the file TakeIn holds open cannot be renamed over, so the swap does not happen there.
//
// What it costs when this fails: a file put at the name after the check is held as the lock.
func TestTakeInRefusesAFileRenamedOverTheNameAfterTheCheck(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := openRoot(t, dir)
	own, err := root.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = own.Close() })
	swapped := 0
	l, err := acquire(in{own}, "app.lock", "app.lock.holder", nil, func() {
		swapped++
		if err := os.WriteFile(filepath.Join(dir, "other"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(dir, "app.lock"), filepath.Join(dir, "first")); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(dir, "other"), filepath.Join(dir, "app.lock")); err != nil {
			t.Fatal(err)
		}
	}, false)
	if swapped != 1 {
		t.Fatalf("swapped %d times", swapped)
	}
	if l != nil || err == nil || IsHeld(err) || !strings.Contains(err.Error(), "app.lock") || !errors.Is(err, errLink) {
		t.Fatalf("acquire: %v, %v, and want errLink naming the file", l, err)
	}
	takeIn(t, root, "first") // the refusal unlocked and closed the file it locked
}

// A pipe at the lock's name below the root is refused and never waited on; one at the note is
// read by HolderIn as a torn note, a writer holding it or not, and a declined TakeIn beside it
// answers at once.
//
// What it costs when this fails: whoever can make a name in the state folder hangs every run.
func TestAPipeBelowTheRootIsRefusedAtTheLockAndReadAsTornAtTheNote(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := openRoot(t, dir)
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe.lock"), 0o600); err != nil {
		t.Fatal(err)
	}
	if l, err := TakeIn(root, "pipe.lock", ""); l != nil || !errors.Is(err, errNotPlain) {
		t.Errorf("TakeIn of a pipe: %v, %v, and want errNotPlain", l, err)
	}
	takeIn(t, root, "app.lock")
	note := filepath.Join(dir, "app.lock.holder")
	if err := syscall.Mkfifo(note, 0o600); err != nil {
		t.Fatal(err)
	}
	if h, raw, err := HolderIn(root, "app.lock.holder"); err != nil || h != (Held{}) || raw != nil {
		t.Errorf("HolderIn of a pipe with no writer: %#v, %q, %v", h, raw, err)
	}
	w, err := os.OpenFile(note, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	if h, raw, err := HolderIn(root, "app.lock.holder"); err != nil || h != (Held{}) || raw != nil {
		t.Errorf("HolderIn of a pipe a writer holds: %#v, %q, %v", h, raw, err)
	}
	var h Held
	if l, err := TakeIn(root, "app.lock", "app.lock.holder"); l != nil || !errors.As(err, &h) || h != (Held{}) {
		t.Errorf("a declined TakeIn beside a pipe a writer holds: %v, %v", l, err)
	}
}

// A note below the root that is a second name of the lock file is refused by Note and Release,
// the lock file left as it is; the note beside the lock file takes its mode; and once the root's
// folder is renamed away while the lock is held, Note and Release still write and remove the note
// in it, never at the old path.
//
// What it costs when this fails: Note takes the lock file's name, or a folder renamed under a
// held lock has the note written where the renamer chose.
func TestNoteAndReleaseBelowTheRootKeepToTheLockFilesFolder(t *testing.T) {
	t.Parallel()
	top := t.TempDir()
	dir := filepath.Join(top, "state")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)
	l := takeIn(t, root, "app.lock")
	if err := os.Chmod(filepath.Join(dir, "app.lock"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(dir, "app.lock"), filepath.Join(dir, "app.lock.holder")); err != nil {
		t.Fatal(err)
	}
	if err := l.Note("mine"); !errors.Is(err, errNoteIsLock) {
		t.Errorf("Note at a second name of the lock file: %v, and want errNoteIsLock", err)
	}
	if err := os.Remove(filepath.Join(dir, "app.lock.holder")); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(top, "moved")
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := l.Note("mine"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(filepath.Join(moved, "app.lock.holder")); err != nil || fi.Mode().Perm() != 0o640 {
		t.Errorf("the note in the folder the root was opened at: %v, %v, and want the lock file's 0640", fi, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "app.lock.holder")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the note at the old path: %v, and want nothing there", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(moved, "app.lock.holder")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the note after Release: %v, and want it removed", err)
	}
	if fi, err := os.Lstat(filepath.Join(moved, "app.lock")); err != nil || fi.Mode().Perm() != 0o640 {
		t.Errorf("the lock file: %v, %v, and want it left as it was", fi, err)
	}
}

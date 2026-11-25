package lock

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// On Windows a note another process holds open, as os.Open opens one, without sharing it for
// delete, is replaced by Note and removed by Release once the reader lets go within about 600 ms;
// held open past that, Release drops the lock all the same and says no error, and the next Take
// wins.
//
// What it costs when this fails: a reader of the note, Holder or a declined Take in another
// process, makes the holder's Note or Release fail, and Release's error reaches a caller that did
// nothing wrong.
func TestANoteHeldOpenByAReaderIsReplacedAndReleasedOnWindows(t *testing.T) {
	t.Parallel()
	path := tempLock(t)
	l := take(t, path)
	if err := l.Note(nil); err != nil {
		t.Fatal(err)
	}
	readFor := func(d time.Duration) *os.File {
		t.Helper()
		f, err := os.Open(path + ".holder")
		if err != nil {
			t.Fatal(err)
		}
		if d > 0 {
			time.AfterFunc(d, func() { _ = f.Close() })
		}
		return f
	}
	readFor(20 * time.Millisecond)
	if err := l.Note("again"); err != nil {
		t.Fatalf("Note while a reader held the note for 20 ms: %v", err)
	}
	if h, note, err := Holder(path + ".holder"); err != nil || h.By != os.Getpid() || string(note) != `"again"` {
		t.Fatalf("Holder: %#v, %s, %v", h, note, err)
	}
	readFor(20 * time.Millisecond)
	if err := l.Release(); err != nil {
		t.Fatalf("Release while a reader held the note for 20 ms: %v", err)
	}
	if _, err := os.Lstat(path + ".holder"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the note outlived the release: %v", err)
	}

	l = take(t, path)
	if err := l.Note(nil); err != nil { // Take writes none, and this part holds a note open past the tries
		t.Fatal(err)
	}
	f := readFor(0)
	err := l.Release()
	_ = f.Close()
	if err != nil {
		t.Errorf("Release while a reader holds the note: %v, and want the lock dropped and no error", err)
	}
	take(t, path)
}

// On Windows a link at the lock path, or at <name>.holder, is opened as the link itself and never
// its target: the target is held open by this test with no sharing at all, so an open of it would
// fail with a sharing violation, not the refusal of a link.
//
// What it costs when this fails: a link put at the lock's name opens its target, read-write as the
// caller, before the refusal, and a named pipe or a remote path there is reached.
func TestALinkIsOpenedAsItselfNeverItsTargetOnWindows(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "precious")
	if err := os.WriteFile(target, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "app.lock")
	plantLink(t, target, path)
	other := filepath.Join(dir, "other.lock")
	take(t, other)
	plantLink(t, target, other+".holder")
	name, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		t.Fatal(err)
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.CloseHandle(h) })
	if l, err := Take(path, path+".holder"); l != nil || !errors.Is(err, errLink) {
		t.Errorf("Take of a link to a file held with no sharing: %v, %v, and want the link refused", l, err)
	}
	if held, note, err := Holder(other + ".holder"); err != nil || held != (Held{}) || note != nil {
		t.Errorf("Holder through a link to a file held with no sharing: %#v, %q, %v", held, note, err)
	}
}

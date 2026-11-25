package lock

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// On a volume that keeps no access lists, a missing lock file is refused with ErrNoAccessList,
// which matches errors.ErrUnsupported, and nothing is made; a lock file there already is taken as
// it was, as nothing is made.
//
// What it costs when this fails: a lock made on a FAT or exFAT volume is every account's to write,
// so another account's run can scribble its note, while the program believes it private;
// or a lock that is there can no longer be taken there at all.
func TestALockFileOnAVolumeWithNoAccessListsIsRefusedAndNotMadeAndOneThereIsTaken(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.lock")
	l, err := acquire(in{}, path, "", nil, nil, true)
	if l != nil {
		_ = l.Release()
	}
	if !errors.Is(err, ErrNoAccessList) || !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Take on a volume with no access lists: %v, and want ErrNoAccessList matching errors.ErrUnsupported", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a refused Take made the lock file: %v", err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	l, err = acquire(in{}, path, "", nil, nil, true)
	if err != nil {
		t.Fatalf("Take of a lock file there, on a volume with no access lists: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
}

// A lock file Take made and still holds cannot be removed or renamed, by this process or any
// other, as one Take opened that was there cannot: each is a sharing violation, and the next Take
// is refused as held. The make shares read and write alone.
//
// What it costs when this fails: another process removes the held lock file, the next Take makes
// a new one at the name and locks it, and two runs hold the lock while the first is never told.
func TestAHeldLockFileItMadeCannotBeRemovedOrRenamedAndTheNextTakeIsRefused(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "app.lock")
	l, err := Take(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := l.Release(); err != nil {
			t.Error(err)
		}
	}()
	if err := os.Remove(path); !errors.Is(err, errSharing) {
		t.Errorf("remove of the held lock file Take made: %v, and want a sharing violation", err)
	}
	if err := os.Rename(path, path+".moved"); !errors.Is(err, errSharing) {
		t.Errorf("rename of the held lock file Take made: %v, and want a sharing violation", err)
	}
	second, err := Take(path, "")
	if second != nil {
		_ = second.Release()
	}
	if !IsHeld(err) {
		t.Errorf("a second Take of the held lock: %v, and want it refused as held", err)
	}
}

// Take of a lock file whose name ends in a dot is refused, matching fs.ErrInvalid, and nothing is
// made: the make would make x.lock. as written, which every later call reads as x.lock.
//
// What it costs when this fails: Take makes a file no later call reaches, its check of the name
// fails, and every later Take of that name fails too, leaving a lock file nothing can open by its
// name.
func TestTakeOfANameEndingInADotIsRefusedAndMakesNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	l, err := Take(filepath.Join(dir, "x.lock."), "")
	if l != nil {
		_ = l.Release()
	}
	if !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("Take of x.lock.: %v, and want a refusal matching fs.ErrInvalid", err)
	}
	if left, err := os.ReadDir(dir); err != nil || len(left) != 0 {
		t.Errorf("a refused Take left %v (%v), and want nothing", left, err)
	}
}

package lock

import (
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

// A contender that finds the lock file there while its maker still has the make's handle open
// takes the lock, or is refused as held: the make's handle (newLock) has no DELETE, so the
// contender's open, which shares read and write alone (openLock), is not refused for sharing.
// Were the make's handle to DELETE, that open would be a sharing violation until the maker
// closed it. The test holds the make's own handle, not a stand-in for it, open through the
// contender's Take.
//
// What it costs when this fails: of two runs that start together on a lock file not yet made, the
// one that loses the create is told neither that it holds the lock nor that another does, and
// Wait does not wait.
func TestAContenderThatMeetsTheMakesHandleIsNotRefusedForSharing(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "app.lock")
	made, err := newLock(in{}, path, false)
	if err != nil {
		t.Fatal(err)
	}
	l, err := Take(path, "")
	if err != nil && !IsHeld(err) {
		t.Errorf("Take while the make's handle is open: %v, and want the lock, or a refusal as held", err)
	}
	if l != nil {
		if err := l.Release(); err != nil {
			t.Error(err)
		}
	}
	if err := made.Close(); err != nil {
		t.Fatal(err)
	}
}

// Of the Takes that start together on a lock file not yet made, each is the lock or a refusal as
// held, whichever of them makes the file: the one that loses the create opens the file while its
// maker may still have the make's handle open.
//
// What it costs when this fails: as above, on the first start of two runs.
func TestTakesOfALockFileNotYetMadeAreEachTheLockOrHeld(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for i := range 500 {
		path := filepath.Join(dir, strconv.Itoa(i)+".lock")
		start := make(chan struct{})
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				<-start
				l, err := Take(path, "")
				if err != nil && !IsHeld(err) {
					t.Errorf("Take %d of a lock file not yet made: %v, and want the lock, or a refusal as held", i, err)
				}
				if l != nil {
					if err := l.Release(); err != nil {
						t.Error(err)
					}
				}
			})
		}
		close(start)
		wg.Wait()
		if t.Failed() {
			return
		}
	}
}

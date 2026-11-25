package lock

import (
	"os"
	"path/filepath"
	"testing"
)

// A Take of a lock file there already makes nothing beside it, for an instant or to keep: the make
// looks for the name before it tries the owner-only list on a throwaway name, finds the lock file, and the Take opens it as it did before that try. The system's record
// of the folder's changes says what was made (made), so a throwaway name made and removed
// at once is seen too.
//
// What it costs when this fails: on a read-only or full volume, where nothing can be made, a Take
// of a lock file there fails where it worked before the look came first, and every Take and Wait makes
// and removes a file beside the lock.
func TestATakeOfALockFileThereMakesNothingBesideIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.lock")
	l, err := Take(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	got := made(t, dir, func() {
		l, err := Take(path, "")
		if err != nil {
			t.Errorf("Take of the lock file there: %v", err)
			return
		}
		if err := l.Release(); err != nil {
			t.Error(err)
		}
	})
	if len(got) != 0 {
		t.Errorf("a Take of the lock file there made %q, and want nothing made", got)
	}
	if left, err := os.ReadDir(dir); err != nil || len(left) != 1 || left[0].Name() != "app.lock" {
		t.Errorf("the Take left %v (%v), and want the lock file alone", left, err)
	}
}

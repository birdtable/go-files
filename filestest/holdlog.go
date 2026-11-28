package filestest

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/birdtable/go-files/internal/loghold"
	"github.com/birdtable/go-files/write"
)

// holdTries is how many times HoldLog opens the log again when another writer moves it between
// HoldLog's look and its hold, before it fails the test.
const holdTries = 8

// HoldLog holds the log at name beneath root as a write.Append in progress holds it, on every
// system, until release is called or the test ends: an Append to it from this process or another
// waits until its ctx ends, and is then refused with ctx's error, found by errors.Is. It is
// Append's own hold, taken by the code Append takes it by, not a lock that stands for it: lock.Take
// keeps Append out on Unix alone. A log that is not there is made empty first, as write.File makes
// a file with no option, so it is the log's own when a later Append moves it aside. HoldLog waits
// for another holder until the test's context ends, and fails the test, saying the name, when the
// log cannot be held: a link or anything else that is not a plain file, a log with a second name,
// a Windows volume with no file index. release may be called more than once; a failed let-go
// fails the test.
func HoldLog(t testing.TB, root *os.Root, name string) (release func()) {
	t.Helper()
	how := loghold.How{Create: func(flag int) (*os.File, error) {
		if err := write.File(filepath.Join(root.Name(), name), nil, write.NoReplace()); err != nil {
			return nil, err // another writer made it first: fs.ErrExist, which Take answers as moved
		}
		return root.OpenFile(name, flag, 0)
	}}
	for range holdTries {
		f, _, _, err := loghold.Take(t.Context(), root, name, how)
		if errors.Is(err, loghold.ErrMoved) {
			continue
		}
		if err != nil {
			t.Fatalf("filestest.HoldLog %s beneath %s: %v", name, root.Name(), err)
		}
		release = sync.OnceFunc(func() {
			if err := loghold.Release(f); err != nil {
				t.Errorf("filestest.HoldLog %s beneath %s: letting go: %v", name, root.Name(), err)
			}
		})
		t.Cleanup(release)
		return release
	}
	t.Fatalf("filestest.HoldLog %s beneath %s: another writer moved the log %d times while this one tried to hold it", name, root.Name(), holdTries)
	return nil
}

package filestest_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/birdtable/go-files/filestest"
	"github.com/birdtable/go-files/trust/trusttest"
	"github.com/birdtable/go-files/write"
)

// privateRoot is a root at a new folder of Private's, closed at the test's end.
func privateRoot(t *testing.T) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(filestest.Private(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	return root
}

// logSays fails the test when the log at name beneath root does not hold exactly want.
func logSays(t *testing.T, root *os.Root, name, want string) {
	t.Helper()
	if b, err := root.ReadFile(name); err != nil || string(b) != want {
		t.Errorf("%s holds %q (%v), and want %q", name, b, err, want)
	}
}

// TestHoldLogKeepsAnAppendOutUntilItIsLetGo runs the example's journal test on each system: an
// Append under the hold ends with its ctx's error and writes nothing, and one after release writes.
// What it costs when this fails: a program's test of a held journal sees its write go through on
// Windows, as lock.Take's hold lets it, and passes without testing the wait at all.
func TestHoldLogKeepsAnAppendOutUntilItIsLetGo(t *testing.T) {
	t.Parallel()
	root := privateRoot(t)
	journalWaitsOnAHeldLog(t, root)
	logSays(t, root, "journal.log", "one\nthree\n")
}

// TestHoldLogMakesALogThatIsNotThereAsAppendWould holds a log no Append has made: it is made empty,
// the library's private file, and held, and an Append after release writes the first line to it.
// What it costs when this fails: a test that holds a journal before its first line holds nothing,
// or a log Append then refuses.
func TestHoldLogMakesALogThatIsNotThereAsAppendWould(t *testing.T) {
	t.Parallel()
	root := privateRoot(t)
	release := filestest.HoldLog(t, root, "a.log")
	fi, err := root.Lstat("a.log")
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != 0 {
		t.Fatalf("a.log is %v (%v) under the hold, and want an empty plain file", fi, err)
	}
	trusttest.Perm(t, filepath.Join(root.Name(), "a.log"), 0o600) // the owner-only list on Windows
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := write.Append(ctx, root, "a.log", []byte("held"), 1<<10, write.Aside("a.log.1")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("an Append to a.log under the hold: %v, and want it to end with its ctx", err)
	}
	release()
	release() // a second call does nothing
	if err := write.Append(t.Context(), root, "a.log", []byte("first"), 1<<10, write.Aside("a.log.1")); err != nil {
		t.Fatal(err)
	}
	logSays(t, root, "a.log", "first\n")
}

// TestHoldLogIsLetGoAtTheTestsEnd holds a log for a test that never calls release: once that
// test's cleanups run, as at its end, an Append to the log writes at once.
// What it costs when this fails: a test that forgets release leaves the log held for every test
// after it in the binary, and theirs fail far from the cause.
func TestHoldLogIsLetGoAtTheTestsEnd(t *testing.T) {
	t.Parallel()
	root := privateRoot(t)
	r := (&recorder{TB: t}).run(func(r *recorder) { filestest.HoldLog(r, root, "a.log") })
	if r.failed {
		t.Fatalf("HoldLog on a new a.log: %q", r.said)
	}
	r.end()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := write.Append(ctx, root, "a.log", []byte("after"), 1<<10, write.Aside("a.log.1")); err != nil {
		t.Fatalf("an Append once the test that held a.log ended: %v, and want it written", err)
	}
	logSays(t, root, "a.log", "after\n")
}

// TestHoldLogFailsTheTestSayingTheNameOfALogAppendRefuses gives HoldLog what Append refuses to write
// to, a folder at the name and a log with a second name: each fails the test, saying the name, and
// is left as it was.
// What it costs when this fails: a test holds a file Append would never write, and its "held"
// journal is a write that goes elsewhere.
func TestHoldLogFailsTheTestSayingTheNameOfALogAppendRefuses(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		make func(t *testing.T, dir string)
		says string
	}{
		{"a folder", func(t *testing.T, dir string) {
			t.Helper()
			if err := os.Mkdir(filepath.Join(dir, "a.log"), 0o700); err != nil {
				t.Fatal(err)
			}
		}, "not a plain file"},
		{"a log with a second name", func(t *testing.T, dir string) {
			t.Helper()
			filestest.WriteFile(t, filepath.Join(dir, "a.log"), []byte("line\n"))
			if err := os.Link(filepath.Join(dir, "a.log"), filepath.Join(dir, "other")); err != nil {
				t.Fatal(err)
			}
		}, "has 2 names"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := privateRoot(t)
			c.make(t, root.Name())
			before, err := root.Lstat("a.log")
			if err != nil {
				t.Fatal(err)
			}
			r := (&recorder{TB: t}).run(func(r *recorder) { filestest.HoldLog(r, root, "a.log") })
			r.end()
			if !r.failed || !r.says("filestest.HoldLog a.log beneath "+root.Name()) || !r.says(c.says) {
				t.Errorf("HoldLog on %s: failed %v, said %q, and want a failure saying a.log and %q", c.name, r.failed, r.said, c.says)
			}
			if after, err := root.Lstat("a.log"); err != nil || !os.SameFile(before, after) || after.Size() != before.Size() {
				t.Errorf("a.log is %v (%v) after the refusal, and want it left as it was", after, err)
			}
		})
	}
}

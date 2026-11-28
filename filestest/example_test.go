package filestest_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/birdtable/go-files/filestest"
	"github.com/birdtable/go-files/write"
)

// journal is a program's journal: one line a call, kept under 1 MiB, the full one moved aside.
func journal(ctx context.Context, root *os.Root, line string) error {
	return write.Append(ctx, root, "journal.log", []byte(line), 1<<20, write.Aside("journal.log.1"))
}

// journalWaitsOnAHeldLog is a program's test of "a journal another process holds is a write that
// waits, and fails when its ctx ends": the journal is held as an Append in progress holds it, on
// every system, and let go of before the test's end.
func journalWaitsOnAHeldLog(t *testing.T, root *os.Root) {
	t.Helper()
	if err := journal(t.Context(), root, "one"); err != nil {
		t.Fatal(err)
	}
	release := filestest.HoldLog(t, root, "journal.log")
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := journal(ctx, root, "two"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a journal line to a held log: %v, and want it to wait and end with its ctx", err)
	}
	release()
	if err := journal(t.Context(), root, "three"); err != nil {
		t.Fatalf("a journal line once the log is let go: %v", err)
	}
}

// A program's test holds its journal as another process's write would, and finds its own write
// waits, then fails when its ctx ends, and succeeds once the hold is let go. journalWaitsOnAHeldLog
// is the test's body; the program's test calls it with its own t and a root of its own:
//
//	func TestJournalWaitsOnAHeldLog(t *testing.T) {
//		root, err := os.OpenRoot(filestest.Private(t))
//		if err != nil {
//			t.Fatal(err)
//		}
//		t.Cleanup(func() { _ = root.Close() })
//		journalWaitsOnAHeldLog(t, root)
//	}
func ExampleHoldLog() {}

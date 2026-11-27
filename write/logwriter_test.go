package write

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// logWriterIn is a LogWriter on name in dir, through a root of dir it closes at the test's end,
// with Aside(name.1).
func logWriterIn(t *testing.T, ctx context.Context, dir, name string, limit int64) *logWriter {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	w, ok := LogWriter(ctx, root, name, limit, Aside(name+".1")).(*logWriter)
	if !ok {
		t.Fatal("LogWriter is not a *logWriter")
	}
	return w
}

// wrote writes p through w and fails the test unless the count is n and the error nil.
func wrote(t *testing.T, w *logWriter, p string, n int) {
	t.Helper()
	if got, err := w.Write([]byte(p)); got != n || err != nil {
		t.Fatalf("Write(%q) = %d, %v, want %d and no error", p, got, err, n)
	}
}

// A Write of one line appends it, a Write of several appends each, an empty line inside one
// included, and each count is the bytes given.
//
// What it costs when this fails: a logger's record is lost, joined to the next, or its empty
// line dropped, and a logger that checks the count reports a short write.
func TestLogWriterAppendsOneLineOrSeveralFromOneWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := filepath.Join(dir, "app.log")
	w := logWriterIn(t, t.Context(), dir, "app.log", 1000)
	wrote(t, w, "one\n", 4)
	holds(t, log, "one\n")
	wrote(t, w, "two\n\nthree\n", 11)
	holds(t, log, "one\ntwo\n\nthree\n")
	wrote(t, w, "\n", 1)
	holds(t, log, "one\ntwo\n\nthree\n\n")
	alone(t, dir, "app.log")
}

// A Write that does not end in a newline, an empty one included, is refused with an error
// matching fs.ErrInvalid, and nothing of it is written then or kept for the next Write.
//
// What it costs when this fails: a half line is appended as a whole one, or held in the writer,
// joined to the next Write's bytes, and lost in a crash.
func TestLogWriterRefusesAWriteWithoutATrailingNewlineAndWritesNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := filepath.Join(dir, "app.log")
	w := logWriterIn(t, t.Context(), dir, "app.log", 1000)
	wrote(t, w, "one\n", 4)
	for _, p := range []string{"two\nthr", "half", ""} {
		if n, err := w.Write([]byte(p)); n != 0 || !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Write(%q) = %d, %v, want 0 and an error matching fs.ErrInvalid", p, n, err)
		}
		holds(t, log, "one\n")
	}
	wrote(t, w, "four\n", 5)
	holds(t, log, "one\nfour\n")
}

// The cap holds through the writer as through Append, line by line within one Write: the line
// that would take the log past it moves the log aside first, and a line longer than it is a log
// of its own, whole.
//
// What it costs when this fails: a slog log grows past its cap and fills the disk, or a record
// is cut across the move.
func TestLogWriterKeepsTheCapLineByLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := filepath.Join(dir, "app.log")
	w := logWriterIn(t, t.Context(), dir, "app.log", 10)
	wrote(t, w, "12345678\nx\n", 11)
	holds(t, log, "x\n")
	holds(t, log+".1", "12345678\n")
	wrote(t, w, "a line longer than the cap\ny\n", 29)
	holds(t, log, "y\n")
	holds(t, log+".1", "a line longer than the cap\n")
	alone(t, dir, "app.log", "app.log.1")
}

// A Write that fails on a line is the bytes of the lines written before it, with Append's error,
// and the lines after it are not written: here the second line would move the log over a folder
// at Aside's name, which Append refuses.
//
// What it costs when this fails: a logger that retries the rest of a short write writes lines
// twice, or past the failed one, out of order.
func TestLogWriterCountsTheBytesBeforeTheFirstFailedLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := filepath.Join(dir, "app.log")
	if err := os.Mkdir(log+".1", 0o700); err != nil {
		t.Fatal(err)
	}
	w := logWriterIn(t, t.Context(), dir, "app.log", 10)
	n, err := w.Write([]byte("abc\n12345678\nz\n"))
	if n != 4 || err == nil {
		t.Fatalf("Write moving the log over a folder = %d, %v, want 4 and Append's error", n, err)
	}
	holds(t, log, "abc\n")
}

// The writer's ctx is every Write's: once it is canceled, a Write to a log another process holds
// is refused at once with context.Canceled and writes nothing, and once the hold is let go the
// same writer writes, as Append tries the hold once whatever its ctx.
//
// What it costs when this fails: a program that ends its context to stop logging is stuck in a
// Write behind any reader that holds the log, for as long as the reader holds it.
func TestLogWriterOnAHeldLogEndsWithItsCanceledContext(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := filepath.Join(dir, "app.log")
	put0(t, log, "old\n")
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	w := logWriterIn(t, canceled, dir, "app.log", 1000)
	letGoOf := holdShared(t, log)
	n, err := w.Write([]byte("line\n")) // the hold is kept until it returns: a Write that waits on it is a hang go test's timeout reports
	if n != 0 || !errors.Is(err, context.Canceled) {
		t.Errorf("Write on a held log with a canceled ctx = %d, %v, want 0 and context.Canceled", n, err)
	}
	holds(t, log, "old\n")
	letGoOf()
	wrote(t, w, "line\n", 5)
	holds(t, log, "old\nline\n")
}

package write

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// LogWriter is an io.Writer over Append, for log/slog's handlers and any other logger that writes
// whole lines to an io.Writer. Each Write must hold one or more complete lines, each ending in a
// newline, and appends each line, without its newline, through Append to the log at name beneath
// root, with ctx, limit and opts, so each line is one write and the cap holds as it does for
// Append. An empty line inside a Write is appended as an empty line.
//
// A Write whose bytes do not end in a newline, an empty one among them, is refused with an error
// matching fs.ErrInvalid, and nothing of it is written: it is never kept back for the next Write,
// as a line held in the writer is a line a crash loses. A Write that fails on a line returns the
// bytes of the lines written before it, with Append's error, and writes none of the lines after.
// Two Writes at once each append their lines one by one, so another writer's line, in this process
// or another, may come between two lines of one Write; slog's handlers call Write once per record.
//
// ctx is the writer's for its whole life, and every line of every Write is appended with it, as an
// io.Writer's Write takes no context of its own: a ctx that ends makes each later Write that meets
// another process's hold on the log fail with ctx's error. A program that wants a deadline per line
// calls Append itself. LogWriter checks nothing when it is called: a name or an option Append
// refuses is refused at each Write.
func LogWriter(ctx context.Context, root *os.Root, name string, limit int64, opts ...LogOption) io.Writer {
	return &logWriter{ctx: ctx, root: root, name: name, limit: limit, opts: opts}
}

type logWriter struct {
	ctx   context.Context // io.Writer's Write takes no context, so the writer keeps its caller's
	root  *os.Root
	name  string
	limit int64
	opts  []LogOption
}

// Write appends each line of p through Append, and is the bytes of the lines written.
func (w *logWriter) Write(p []byte) (int, error) {
	if len(p) == 0 || p[len(p)-1] != '\n' {
		return 0, fmt.Errorf("%s beneath %s: a Write of %d bytes does not end in a newline, so none is written: %w", w.name, w.root.Name(), len(p), fs.ErrInvalid)
	}
	n := 0
	for line := range bytes.Lines(p) {
		if err := Append(w.ctx, w.root, w.name, line[:len(line)-1], w.limit, w.opts...); err != nil {
			return n, err
		}
		n += len(line)
	}
	return n, nil
}

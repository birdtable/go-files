package write

import (
	"path/filepath"
	"testing"
)

// A torn last line, left by a crash in another writer's write, is ended before the next line, in
// the same write, and kept as it is; a log that ends in a newline takes no second one; the newline
// counts toward the cap, so a torn log one byte short of room moves aside with its torn line.
//
// What it costs when this fails: the new line joins the half-written one, and a reader parses one
// line that is neither; or every line gets an empty one before it; or the log grows a byte past
// its cap.
func TestAppendEndsATornLastLineFirst(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, before, line string
		limit              int64
		log, old           string
	}{
		{"a torn line", "first\nhal", "next", 100, "first\nhal\nnext\n", ""},
		{"a log of one torn line", "hal", "next", 100, "hal\nnext\n", ""},
		{"a whole last line", "first\n", "next", 100, "first\nnext\n", ""},
		{"a torn line at the cap exactly", "abc", "xyz", 8, "abc\nxyz\n", ""},
		{"a torn line one byte past the cap", "abc", "xyz", 7, "xyz\n", "abc"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			log := filepath.Join(dir, "audit.log")
			put0(t, log, c.before)
			if err := appendIn(t, dir, "audit.log", c.line, c.limit); err != nil {
				t.Fatal(err)
			}
			holds(t, log, c.log)
			if c.old != "" {
				holds(t, log+".1", c.old)
			}
		})
	}
}

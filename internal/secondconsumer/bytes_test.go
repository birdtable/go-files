package secondconsumer_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/write"
)

// Each firing note is written by write.File, write.FileIn and write.FileFrom, with no option, with
// Runnable and with Shared, and read back the very bytes it was handed: nothing in go-files reads
// or changes content, on any system.
//
// What it costs when this fails: a file the watcher generates differs by system, its line endings
// changed or a byte order mark taken off, and a byte compare of it, or its digest, fails on one
// system alone.
func TestTheWatchersFiringNotesAreWrittenByteForByteByEveryWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	// The watcher's firing notes as its operators type them on every system: one ends its lines in
	// \n, one in \r\n, one in a lone \r, one mixes the three, one has no final newline, one holds a
	// NUL, one starts with a UTF-8 byte order mark, one holds bytes that are not UTF-8, and one is
	// empty.
	firingNotes := []struct{ what, body string }{
		{"\\n", "cone 6\nhold 20\n"},
		{"\\r\\n", "cone 6\r\nhold 20\r\n"},
		{"a lone \\r", "cone 6\rhold 20\r"},
		{"mixed endings", "cone 6\r\nhold 20\nvent\rcool\n"},
		{"no final newline", "cone 6\nhold 20"},
		{"a NUL", "cone\x006\n"},
		{"a UTF-8 byte order mark", "\xef\xbb\xbfcone 6\n"},
		{"bytes that are not UTF-8", "cone \xff\xfe\xc3\x28 6\n"},
		{"empty", ""},
	}
	options := []struct {
		what string
		opts []write.Option
	}{{"no option", nil}, {"Runnable", []write.Option{write.Runnable()}}, {"Shared", []write.Option{write.Shared()}}}
	for i, note := range firingNotes {
		for j, o := range options {
			want := []byte(note.body)
			name := func(by string) string { return fmt.Sprintf("firing-%d-%d-%s.note", i, j, by) }
			check := func(by string, werr error, got []byte, rerr error) {
				t.Helper()
				switch {
				case werr != nil:
					t.Errorf("%s by %s with %s: %v", note.what, by, o.what, werr)
				case rerr != nil:
					t.Errorf("%s by %s with %s, read back: %v", note.what, by, o.what, rerr)
				case !bytes.Equal(got, want):
					t.Errorf("%s by %s with %s reads back %q, and want %q", note.what, by, o.what, got, want)
				}
			}

			path := filepath.Join(dir, name("File"))
			werr := write.File(path, want, o.opts...)
			got, rerr := os.ReadFile(path)
			check("write.File", werr, got, rerr)

			werr = write.FileIn(root, name("FileIn"), want, o.opts...)
			got, rerr = root.ReadFile(name("FileIn"))
			check("write.FileIn", werr, got, rerr)

			path = filepath.Join(dir, name("FileFrom"))
			werr = write.FileFrom(path, bytes.NewReader(want), o.opts...)
			got, rerr = os.ReadFile(path)
			check("write.FileFrom", werr, got, rerr)
		}
	}
}

//go:build windows

package trust

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Windows has no /proc, and os's open of a name passes no quality of service there, so trust's
// readProc on Windows opens nothing: it answers errors.ErrUnsupported and no bytes for a file that
// is there and holds a uid_map's line. The control reads that file by os.ReadFile and finds the line.
//
// What it costs when this fails: a read trust would make of /proc/self/uid_map on Windows, on the
// current drive where any account may make the folder \proc, follows a junction planted there to a
// pipe, whose server may then act as the program.
func TestOnWindowsReadProcOpensNothingAndAnswersErrUnsupported(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "uid_map")
	line := []byte("0 1000 1\n")
	if err := os.WriteFile(path, line, 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != string(line) {
		t.Fatalf("the control: os.ReadFile of %s: %q, %v, and want %q", path, b, err, line)
	}
	b, err := readProc(path)
	if b != nil || !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("readProc of %s: %q, %v, and want no bytes and errors.ErrUnsupported", path, b, err)
	}
}

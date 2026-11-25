package trust

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/internal/fileid"
)

// TestIdentRefusesANoIndexFile: ident reads a file's identity by file ID, which os.SameFile compares; on
// a Windows volume whose files read a zero index (a WebDAV or SMB drive) or an all-ones one (ReFS) it
// refuses with fileid.ErrNoFileIndex, one value with write.ErrNoFileIndex, so errors.Is holds with it,
// rather than hand back an ID that tells one file from none. The seam makes a normal volume read as one
// with no index; removing the fileid check by hand turns this row red.
//
// What it costs when this fails: trust walks a path on such a volume, takes two files for the same, and
// may judge one file by another's owner and access list.
func TestIdentRefusesANoIndexFile(t *testing.T) { //nolint:paralleltest // serial: SeamNoIndex moves shared state in internal/fileid
	// serial: SeamNoIndex moves shared state in internal/fileid
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer fileid.SeamNoIndex()()
	if _, err := ident(path); !errors.Is(err, fileid.ErrNoFileIndex) || !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("ident on a volume that gives a file no index: %v, want fileid.ErrNoFileIndex (matching ErrUnsupported)", err)
	}
}

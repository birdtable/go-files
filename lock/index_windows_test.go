package lock

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/internal/fileid"
)

// TestANoIndexVolumeIsRefusedWhenTheLockFileIsCompared: on a Windows volume whose files read a zero or
// all-ones index, os.SameFile cannot tell the lock file from another at its name, so atName and notLock
// refuse with fileid.ErrNoFileIndex, one value with write.ErrNoFileIndex, so errors.Is holds with it,
// rather than trust the compare. The seam makes a normal volume read as one with no index; removing the
// indexAt call by hand turns this row red.
//
// What it costs when this fails: on a WebDAV or SMB drive or on ReFS a file swapped at the lock's name
// passes the identity compare, so two runs share one lock or a note is taken for the lock file.
func TestANoIndexVolumeIsRefusedWhenTheLockFileIsCompared(t *testing.T) { //nolint:paralleltest // serial: SeamNoIndex moves shared state in internal/fileid
	// serial: SeamNoIndex moves shared state in internal/fileid
	dir := t.TempDir()
	path := filepath.Join(dir, "app.lock")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	defer fileid.SeamNoIndex()()
	if err := atName(in{}, path, f); !errors.Is(err, fileid.ErrNoFileIndex) || !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("atName on a volume that gives the lock file no index: %v, want fileid.ErrNoFileIndex (matching ErrUnsupported)", err)
	}
	if err := notLock(in{}, path, f); !errors.Is(err, fileid.ErrNoFileIndex) {
		t.Errorf("notLock on a volume that gives the lock file no index: %v, want fileid.ErrNoFileIndex", err)
	}
}

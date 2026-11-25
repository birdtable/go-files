package appdirs_test

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/appdirs"
)

// Free of a folder that is there is some room and no error, and of a folder that is not is an
// *fs.PathError matching fs.ErrNotExist, naming the path.
//
// What it costs when this fails: a program that checks for room before a large write is told a
// missing folder has none, or has room it reads as zero, and refuses a write it could make.
func TestFreeOfAFolderIsItsRoomAndOfAMissingOneAnError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	n, err := appdirs.Free(dir)
	if err != nil || n == 0 {
		t.Fatalf("Free(the temp folder) = %d, %v, want some room", n, err)
	}
	t.Logf("Free(the temp folder) = %d bytes, %d KiB", n, n/1024)
	missing := filepath.Join(dir, "missing")
	n, err = appdirs.Free(missing)
	var pe *fs.PathError
	if !errors.Is(err, fs.ErrNotExist) || !errors.As(err, &pe) || pe.Path != missing || n != 0 {
		t.Errorf("Free(missing) = %d, %v, want an *fs.PathError naming it, matching fs.ErrNotExist", n, err)
	}
}

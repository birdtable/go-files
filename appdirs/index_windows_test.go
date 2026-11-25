package appdirs

import (
	"errors"
	"os"
	"testing"

	"github.com/birdtable/go-files/internal/fileid"
)

// TestOnWayRefusesANoIndexVolume: the apart-walk reads each folder on the way by file ID, which
// os.SameFile compares; on a Windows volume whose folders read a zero or all-ones index it refuses with
// fileid.ErrNoFileIndex, one value with write.ErrNoFileIndex, so errors.Is holds with it, rather than
// trust the compare. The seam makes a normal volume read as one with no index; removing the indexAtPath
// call by hand turns this row red.
//
// What it costs when this fails: on such a drive the temp folder is read as holding, or not holding, a
// folder it is kept apart from, by a file-ID compare that cannot tell two folders apart.
func TestOnWayRefusesANoIndexVolume(t *testing.T) { //nolint:paralleltest // serial: SeamNoIndex moves shared state in internal/fileid
	// serial: SeamNoIndex moves shared state in internal/fileid
	dir := t.TempDir()
	d, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fileid.SeamNoIndex()()
	_, err = onWay(dir, d)
	if !errors.Is(err, fileid.ErrNoFileIndex) || !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("onWay on a volume that gives a folder no index: %v, want fileid.ErrNoFileIndex (matching ErrUnsupported)", err)
	}
}

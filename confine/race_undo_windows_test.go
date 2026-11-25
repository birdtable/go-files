package confine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// plantFolderLink makes dir's link lead to its folder target by a junction, which any account may
// make, so a race that swaps a folder for a link runs with Developer Mode on or off.
func plantFolderLink(t *testing.T, dir, target, link string) {
	t.Helper()
	plantJunction(t, filepath.Join(dir, target), filepath.Join(dir, link))
}

// undoSwap is where a race undoes stepOpen's swap, at stepMade, and it cannot on Windows: the walk
// holds open the file it made in d-was, and NTFS refuses to rename a folder with a file open in it,
// Access is denied, whatever the file's share. So it checks that d-was cannot be moved, leaves the
// link at d, and answers false: the second walk then meets the link.
func undoSwap(t *testing.T, dir string) bool {
	t.Helper()
	if err := os.Rename(filepath.Join(dir, "d-was"), filepath.Join(dir, "d-back")); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("d-was, holding the file the walk made, moved: %v, and want Access is denied", err)
	}
	return false
}

//go:build !windows

package confine

import (
	"os"
	"path/filepath"
	"testing"
)

// plantFolderLink makes dir's link a symbolic link to its folder target.
func plantFolderLink(t *testing.T, dir, target, link string) {
	t.Helper()
	plantLink(t, target, filepath.Join(dir, link))
}

// undoSwap undoes stepOpen's swap at stepMade, the link at d removed and d-was moved back to d,
// while the walk holds the file it made in d-was, and answers true.
func undoSwap(t *testing.T, dir string) bool {
	t.Helper()
	must(t, os.Remove(filepath.Join(dir, "d")))
	must(t, os.Rename(filepath.Join(dir, "d-was"), filepath.Join(dir, "d")))
	return true
}

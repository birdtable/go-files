//go:build windows

package confine

import (
	"path/filepath"
	"testing"
)

// On Windows RemoveAll itself removes a tree whose folders and files have the read-only attribute,
// as os.Root's removal ignores it, so RemoveAllOwned is RemoveAll there and changes no attribute.
//
// What it costs when this fails: RemoveAllOwned's doc promises a Windows removal that stops at the
// first read-only folder, and a run's home is left behind there.
func TestRemoveAllRemovesAReadOnlyTreeOnWindows(t *testing.T) {
	t.Parallel()
	root, dir := removeFolder(t)
	readOnly(t, dir, "tree/a.txt", "tree/sub/b.txt", "tree/sub/deeper", "tree/sub", "tree", "empty")
	for _, name := range []string{"tree", "empty"} {
		if err := RemoveAll(root, name); err != nil {
			t.Errorf("RemoveAll(%s): %v", name, err)
		}
		gone(t, dir, name)
	}
	holds(t, dir, filepath.Join("real", "HEAD"), "head")
}

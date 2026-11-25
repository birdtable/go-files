//go:build unix

package confine

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// os.Root's refusal of a path that leads outside it, through a link at a middle component, is
// matched by escapes, which a walk that saw a folder there a moment before then names as ErrOutside;
// a missing name and no error are not. The race of swap_race_test.go is where the walk meets it.
//
// What it costs when this fails: a folder swapped for a link to outside the root, mid-walk, is
// reported as a plain failure a program retries, not as a refusal.
func TestOSRootsEscapeIsMatchedAndOthersAreNot(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	in := filepath.Join(base, "in")
	must(t, os.MkdirAll(filepath.Join(base, "out"), 0o755))
	must(t, os.WriteFile(filepath.Join(base, "out", "f.txt"), nil, 0o644))
	must(t, os.Mkdir(in, 0o755))
	plantLink(t, filepath.Join("..", "out"), filepath.Join(in, "l"))
	root, err := os.OpenRoot(in)
	must(t, err)
	defer root.Close()
	_, through := root.Lstat(filepath.Join("l", "f.txt"))
	_, missing := root.Lstat("gone")
	if through == nil || !escapes(root, through) {
		t.Errorf("os.Root through l: %v, want it matched as an escape", through)
	}
	if escapes(root, missing) || escapes(root, nil) || escapes(root, fs.ErrPermission) {
		t.Errorf("escapes matched %v, nil or fs.ErrPermission", missing)
	}
}

package confine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// Mkdir over folders there already makes nothing in any of them, for an instant or to keep: each
// step's make looks for its folder before it tries the owner-only list on a throwaway name,
// and answers fs.ErrExist, so the step walks into the folder there.
// The system's record of the folders' changes says what was made (made), so a throwaway
// name made and removed at once is seen too.
//
// What it costs when this fails: on a read-only or full volume, where nothing can be made, Mkdir
// over folders there fails where it worked before the look came first, and every step over a folder there
// makes and removes a folder in the one above.
func TestMkdirOverFoldersThereAnswersEachAsThereAndMakesNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := rootAt(t, dir)
	r, err := Mkdir(root, filepath.Join("a", "b"), 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	var mkErr error
	got := made(t, dir, func() {
		mkErr = newFolder(root, "a", 0o700, ownerOnly, false)
		r, err := Mkdir(root, filepath.Join("a", "b"), 0o700)
		if err != nil {
			t.Errorf("Mkdir over the folders there: %v", err)
			return
		}
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	if !errors.Is(mkErr, fs.ErrExist) {
		t.Errorf("the make of the folder a, there already: %v, and want fs.ErrExist", mkErr)
	}
	if len(got) != 0 {
		t.Errorf("Mkdir over the folders there made %q, and want nothing made", got)
	}
	for folder, want := range map[string]string{dir: "a", filepath.Join(dir, "a"): "b"} {
		if left, err := os.ReadDir(folder); err != nil || len(left) != 1 || left[0].Name() != want {
			t.Errorf("%s holds %v (%v), and want %s alone", folder, left, err, want)
		}
	}
	if left, err := os.ReadDir(filepath.Join(dir, "a", "b")); err != nil || len(left) != 0 {
		t.Errorf("the folder b holds %v (%v), and want nothing", left, err)
	}
}

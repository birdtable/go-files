//go:build unix

package confine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// What it costs when this fails: a folder this account may not search, or a loop of links on the
// way, is taken for a folder with no marker, and a marker farther up, which another account may
// have planted, is answered.
func TestOnUnixNearestStopsAtAFolderItCannotSearchAndAtALoop(t *testing.T) {
	t.Parallel()
	tmp, start := nearestTree(t)
	b := filepath.Join(tmp, "a", "b")
	put(t, filepath.Join(tmp, "a", "mark"))
	if err := os.Chmod(b, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(b, 0o700) })
	got, err := confine.Nearest(start, "mark", tmp)
	if os.Geteuid() == 0 {
		// root searches a folder whatever its mode, so the walk sees no denial and goes on up.
		if err != nil || got != filepath.Join(tmp, "a") {
			t.Errorf("as root, past a folder with no search bit: %q, %v", got, err)
		}
	} else if !errors.Is(err, fs.ErrPermission) || got != "" {
		t.Errorf("below a folder with no search bit: %q, %v, want fs.ErrPermission", got, err)
	}

	loop := filepath.Join(tmp, "loop")
	put(t, filepath.Join(tmp, "mark"))
	plantLink(t, "loop", loop)
	got, err = confine.Nearest(filepath.Join(loop, "c"), "mark", tmp)
	if !errors.Is(err, syscall.ELOOP) || got != "" {
		t.Errorf("below a loop of links: %q, %v, want ELOOP", got, err)
	}
}

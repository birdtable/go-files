package confine_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// What it costs when this fails: a junction in the store, which any account makes with no
// privilege, has a file outside it counted as the store's.
func TestNamesOnWindowsRefusesAJunctionOnTheWay(t *testing.T) {
	t.Parallel()
	in, out := t.TempDir(), t.TempDir()
	writeMode(t, filepath.Join(out, "a"), 0o644)
	plantJunction(t, out, filepath.Join(in, "j"))
	n, err := confine.Names(openRoot(t, in), filepath.Join("j", "a"))
	if n != 0 || !errors.Is(err, confine.ErrOutside) {
		t.Errorf(`j\a through a junction: %d, %v, and want 0 and ErrOutside`, n, err)
	}
	if _, err := os.Stat(filepath.Join(in, "j", "a")); err != nil {
		t.Errorf("the junction does not lead to the file, so the row proves nothing: %v", err)
	}
}

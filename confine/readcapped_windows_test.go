package confine_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// What it costs when this fails: a junction in the repository, which any account makes with no
// privilege, hands a program the bytes of a file outside it as the repository's.
func TestReadCappedOnWindowsRefusesAJunctionOnTheWay(t *testing.T) {
	t.Parallel()
	in, out := t.TempDir(), t.TempDir()
	writeMode(t, filepath.Join(out, "secret"), 0o600)
	plantJunction(t, out, filepath.Join(in, "j"))
	got, err := confine.ReadCapped(openRoot(t, in), filepath.Join("j", "secret"), 64)
	if got != nil || !errors.Is(err, confine.ErrOutside) {
		t.Errorf(`j\secret through a junction: %q, %v, and want nothing and ErrOutside`, got, err)
	}
	if _, err := os.Stat(filepath.Join(in, "j", "secret")); err != nil {
		t.Errorf("the junction does not lead to the file, so the row proves nothing: %v", err)
	}
}

//go:build unix || windows

package appdirs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/trust"
)

// What it costs when this fails: TempOverflow, which trusts the kernel's overflow account as root,
// does so off Linux, or on Linux where the uid_map maps root, where that account is any one the
// namespace leaves out; or a refusal makes the folder anyway.
func TestTempOverflowRefusesOffLinuxAndWhereTheUIDMapMapsRoot(t *testing.T) {
	t.Parallel()
	if overflowTaken && !uidMapMapsRoot(t) {
		t.Fatal("the test tier runs in a user namespace that maps no root; this test wants one that maps root, or none")
	}
	const app = "go-files-temp-overflow"
	got, err := TempOverflow(app)
	if !errors.Is(err, trust.ErrOverflow) || errors.Is(err, errors.ErrUnsupported) == overflowTaken {
		t.Errorf("TempOverflow = %q, %v, and want trust.ErrOverflow, matching errors.ErrUnsupported %v", got, err, !overflowTaken)
	}
	tmp, err := realPath(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	absent(t, filepath.Join(tmp, tempName(app)))
}

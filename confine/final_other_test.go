//go:build !linux && !darwin && !windows

package confine

import (
	"errors"
	"testing"
)

// finalFoldersOnly is whether Final answers only a folder here, and refuses a file: no system here answers Final at all.
const finalFoldersOnly = false

func TestFinalIsErrUnsupportedOffLinuxMacOSAndWindows(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	if _, err := Final(tmp); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Final(%q) on a system with no call that names an open file: %v, and want errors.ErrUnsupported", tmp, err)
	}
}

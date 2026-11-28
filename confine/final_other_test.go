//go:build !linux && !darwin && !windows

package confine

import (
	"errors"
	"testing"
)

func TestFinalIsErrUnsupportedOffLinuxMacOSAndWindows(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	if _, err := Final(tmp); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Final(%q) on a system with no call that names an open file: %v, and want errors.ErrUnsupported", tmp, err)
	}
}

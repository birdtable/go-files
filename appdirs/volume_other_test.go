//go:build !darwin && !linux && !windows

package appdirs_test

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/birdtable/go-files/appdirs"
)

// On a system Volume does not ask, the temp folder is an *fs.PathError naming it, matching
// errors.ErrUnsupported, with the zero VolumeInfo, which is not local.
//
// What it costs when this fails: a program on a system Volume cannot read is told its folder is
// on some volume, or is told nothing it can tell from a missing folder.
func TestVolumeIsUnsupportedOnOtherSystems(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got, err := appdirs.Volume(dir)
	if pe, ok := errors.AsType[*fs.PathError](err); !ok || pe.Path != dir || !errors.Is(err, errors.ErrUnsupported) || got != (appdirs.VolumeInfo{}) || got.Local() {
		t.Errorf("Volume(%s) = %+v, %v, want the zero VolumeInfo and an *fs.PathError matching errors.ErrUnsupported", dir, got, err)
	}
}

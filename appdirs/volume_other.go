//go:build !darwin && !linux && !windows

package appdirs

import (
	"errors"
	"fmt"
	"io/fs"
)

// askVolume refuses on a system Volume does not ask, with an *fs.PathError matching
// errors.ErrUnsupported.
func askVolume(path, _ string) (VolumeInfo, error) {
	return VolumeInfo{}, &fs.PathError{Op: opVolume, Path: path, Err: fmt.Errorf("appdirs cannot tell a volume's kind on this system: %w", errors.ErrUnsupported)}
}

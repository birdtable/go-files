//go:build !linux && !darwin && !freebsd && !windows

package appdirs

import (
	"errors"
	"fmt"
	"io/fs"
)

// Free refuses on a system it has no call for, with an *fs.PathError matching
// errors.ErrUnsupported.
func Free(path string) (uint64, error) {
	return 0, &fs.PathError{Op: opFree, Path: path, Err: fmt.Errorf("appdirs cannot read the room left on this system: %w", errors.ErrUnsupported)}
}

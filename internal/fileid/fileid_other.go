//go:build !windows

package fileid

import "os"

// deviceNames is false off Windows: no path names a device by its spelling, and a backslash is a
// name's own character.
const deviceNames = false

// Indexed is (true, nil) off Windows: every file has an inode os.SameFile can trust, so no volume is
// refused for want of a file index and the forceNoIndex seam has nothing to act on.
func Indexed(_ *os.File, _ bool) (bool, error) { return true, nil }

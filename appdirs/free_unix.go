//go:build linux || darwin || freebsd

package appdirs

import (
	"io/fs"
	"math"

	"golang.org/x/sys/unix"
)

// Free is the bytes free to this account on the file system that holds path, which leaves out
// what the system keeps back for root. A path that is not there is an *fs.PathError matching
// fs.ErrNotExist.
func Free(path string) (uint64, error) {
	var s unix.Statfs_t
	if err := unix.Statfs(path, &s); err != nil {
		return 0, &fs.PathError{Op: opFree, Path: path, Err: err}
	}
	return room(s.Bavail, s.Bsize), nil
}

// room is avail blocks of bsize bytes: none when either is below one, and at most MaxUint64.
func room[A, B int32 | int64 | uint32 | uint64](avail A, bsize B) uint64 {
	if avail <= 0 || bsize <= 0 {
		return 0
	}
	a, b := uint64(avail), uint64(bsize)
	if a > math.MaxUint64/b {
		return math.MaxUint64
	}
	return a * b
}

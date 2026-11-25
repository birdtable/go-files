package trust

import (
	"errors"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// aclMask is whether path holds a POSIX access list. Linux keeps one as an attribute and drops it
// when it says no more than the mode, so one there names an account or group, and the mode's group
// bits are its mask. No attribute, or a file system with no lists, is none; any other error is an
// error, so the name is not judged.
func aclMask(path string) (bool, error) {
	_, err := unix.Lgetxattr(path, "system.posix_acl_access", nil)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, unix.ENODATA), errors.Is(err, unix.ENOTSUP):
		return false, nil
	}
	return false, &os.PathError{Op: "getxattr", Path: path, Err: err}
}

// aclMaskFile is aclMask of the file f holds, read on its descriptor with fgetxattr, never by a
// name, for Held.
func aclMaskFile(f *os.File) (bool, error) {
	c, err := f.SyscallConn()
	if err != nil {
		return false, err
	}
	var gerr error
	if cerr := c.Control(func(fd uintptr) { _, gerr = unix.Fgetxattr(int(fd), "system.posix_acl_access", nil) }); cerr != nil {
		return false, cerr
	}
	switch {
	case gerr == nil:
		return true, nil
	case errors.Is(gerr, unix.ENODATA), errors.Is(gerr, unix.ENOTSUP):
		return false, nil
	}
	return false, &os.PathError{Op: "fgetxattr", Path: f.Name(), Err: gerr}
}

const overflowUID = "/proc/sys/kernel/overflowuid" // the overflow account

// listOf is n on Linux, whose access list shows in the mode's group bits and is read by aclMask.
func listOf(n node, _ string, _ fs.FileInfo) (node, error) { return n, nil }

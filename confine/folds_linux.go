package confine

import (
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// askFolding asks the file system of f, the folder root opened, its type, and the folder its
// attributes where the type keeps casefold.
func askFolding(f *os.File, root string) (Folding, error) {
	c, err := f.SyscallConn()
	if err != nil {
		return FoldUnknown, err
	}
	var st unix.Statfs_t
	var flags uint32
	var serr, ierr error
	if err := c.Control(func(fd uintptr) {
		if serr = unix.Fstatfs(int(fd), &st); serr == nil && casefolds(uint32(st.Type)) { //nolint:gosec // G115: a magic number is the low 32 bits of f_type
			flags, ierr = unix.IoctlGetUint32(int(fd), unix.FS_IOC_GETFLAGS)
		}
	}); err != nil {
		return FoldUnknown, err
	}
	if serr != nil {
		return FoldUnknown, &fs.PathError{Op: "fstatfs", Path: root, Err: serr}
	}
	if ierr != nil {
		ierr = &fs.PathError{Op: "ioctl FS_IOC_GETFLAGS", Path: root, Err: ierr}
	}
	return linuxFolding(root, uint32(st.Type), flags, ierr) //nolint:gosec // G115: a magic number is the low 32 bits of f_type
}

package procfs

import (
	"io/fs"

	"golang.org/x/sys/unix"
)

// rootIno is the inode of a procfs's root folder, PROC_ROOT_INO.
const rootIno = 1

// Root is path, /proc where a test passes no other, opened O_PATH and held to a procfs's root by
// fstatfs's magic and its inode, so a file system mounted there, or a folder of a procfs bound
// there, is ErrNotProc, in an *fs.PathError of op fstatfs. The caller closes the descriptor.
func Root(path string) (int, error) {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	var sf unix.Statfs_t
	var st unix.Stat_t
	if unix.Fstatfs(fd, &sf) != nil || uint32(sf.Type) != unix.PROC_SUPER_MAGIC || unix.Fstat(fd, &st) != nil || st.Ino != rootIno { //nolint:gosec // G115: a magic number is the low 32 bits of f_type
		unix.Close(fd) //nolint:errcheck,gosec // G104: a descriptor not kept
		return -1, &fs.PathError{Op: "fstatfs", Path: path, Err: ErrNotProc}
	}
	return fd, nil
}

// Beneath opens name beneath root, a descriptor Root answered, with flags, by openat2 with
// RESOLVE_NO_XDEV and RESOLVE_NO_MAGICLINKS: it crosses no mount, so one over any folder or file on
// the way is EXDEV, and follows no magic link, such as self/cwd, which is ELOOP. self, procfs's
// plain link to this process's folder, is followed. A kernel with no openat2, before Linux 5.6, or
// a seccomp filter that refuses it, is ENOSYS. The error is openat2's alone, for the caller to wrap.
func Beneath(root int, name string, flags uint64) (int, error) {
	how := unix.OpenHow{Flags: flags, Resolve: unix.RESOLVE_NO_XDEV | unix.RESOLVE_NO_MAGICLINKS}
	return unix.Openat2(root, name, &how)
}

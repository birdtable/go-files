package confine

import (
	"errors"
	"io/fs"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// openFinal opens path for its name alone, with O_PATH, following no link at its last name.
// O_PATH reads nothing, so a file this account may not read, a pipe and a device open, and none
// waits or acts.
func openFinal(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	for errors.Is(err, unix.EINTR) {
		fd, err = unix.Open(path, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	}
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

// finalOf reads the kernel's name for f from /proc/self/fd.
func finalOf(f *os.File) (string, error) {
	return procName("/proc/self/fd", f)
}

// finalHeld refuses nothing on Linux: a file with a second name answers the name it was opened by.
func finalHeld(fs.FileInfo) error { return nil }

// errNotProcFds is a /proc/self/fd that fstatfs does not show is a procfs: an owner of this mount
// namespace may mount over that path, and a link there then answers what that owner wrote.
var errNotProcFds = errors.New("is not shown by fstatfs to be a procfs, so its links are not the kernel's")

// procName reads the link <n> for f's descriptor n in the folder fds, as trust holds /proc: fds is
// opened, held to a procfs by fstatfs's magic, and the link read under that handle with
// readlinkat. A test passes another fds.
func procName(fds string, f *os.File) (string, error) {
	dir, err := unix.Open(fds, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	for errors.Is(err, unix.EINTR) {
		dir, err = unix.Open(fds, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	}
	if err != nil {
		return "", &fs.PathError{Op: "open", Path: fds, Err: err}
	}
	defer unix.Close(dir) //nolint:errcheck // an O_PATH descriptor, only read under
	var sf unix.Statfs_t
	if err := unix.Fstatfs(dir, &sf); err != nil {
		return "", &fs.PathError{Op: "fstatfs", Path: fds, Err: err}
	}
	if uint32(sf.Type) != unix.PROC_SUPER_MAGIC { //nolint:gosec // G115: a magic number is the low 32 bits of f_type
		return "", &fs.PathError{Op: "fstatfs", Path: fds, Err: errNotProcFds}
	}
	n := strconv.FormatUint(uint64(f.Fd()), 10)
	for size := 256; ; size *= 2 {
		buf := make([]byte, size)
		got, err := unix.Readlinkat(dir, n, buf)
		if err != nil {
			return "", &fs.PathError{Op: "readlinkat", Path: fds + "/" + n, Err: err}
		}
		if got < size {
			return string(buf[:got]), nil
		}
	}
}

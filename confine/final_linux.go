package confine

import (
	"errors"
	"io/fs"
	"os"
	"strconv"

	"github.com/birdtable/go-files/internal/procfs"
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

// finalOf reads the kernel's name for f from /proc/self/fd, by procName.
func finalOf(f *os.File) (string, error) {
	return procName("/proc", f)
}

// finalHeld refuses nothing on Linux: a file with a second name answers the name it was opened by.
func finalHeld(fs.FileInfo) error { return nil }

// procName reads the link self/fd/<n> under proc for f's descriptor n, as trust reads
// self/mountinfo: proc is opened and held to a procfs's root by fstatfs's magic and its inode,
// self/fd is opened beneath it by openat2, crossing no mount and following no magic link, and the
// link is read under that handle with readlinkat. A mount over /proc, over /proc/<pid> or over its
// fd folder, as an owner of this mount namespace may make, is refused, and so is a kernel with no
// openat2. A test passes another proc.
func procName(proc string, f *os.File) (string, error) {
	root, err := procfs.Root(proc)
	if err != nil {
		return "", err
	}
	defer unix.Close(root) //nolint:errcheck // an O_PATH descriptor, only read under
	fds := proc + "/self/fd"
	dir, err := procfs.Beneath(root, "self/fd", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC)
	if err != nil {
		return "", &fs.PathError{Op: "openat2", Path: fds, Err: err}
	}
	defer unix.Close(dir) //nolint:errcheck // an O_PATH descriptor, only read under
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

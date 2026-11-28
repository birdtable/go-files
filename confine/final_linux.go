package confine

import (
	"errors"
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

// procName reads the link fds/<n> for f's descriptor n. A test passes another fds.
func procName(fds string, f *os.File) (string, error) {
	return os.Readlink(fds + "/" + strconv.FormatUint(uint64(f.Fd()), 10))
}

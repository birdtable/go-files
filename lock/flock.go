//go:build darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd

// The systems above are those that lock by flock(2). On aix and solaris the
// one lock is fcntl's, which a second open of the file in the same process
// wins and its close drops, so lock builds there no more than it did.

package lock

import (
	"errors"
	"os"
	"syscall"
)

// tryLock takes flock(2) on f, the file openLock opened and checked, and is
// false while another holds it.
func tryLock(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

// unlock drops f's flock before the close, so a fork on its way to exec that
// shares f does not keep it.
func unlock(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }

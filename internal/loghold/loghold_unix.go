//go:build !windows

package loghold

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

const noWait = syscall.O_NONBLOCK // a pipe at the name answers at once, and its stat refuses it

func tryHold(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

// letGo drops f's flock before the close, so a fork on its way to exec that shares f does not keep it.
func letGo(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }

func names(_ *os.File, fi fs.FileInfo) (uint64, error) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("%s: this system does not say how many names a file has, and no line is written to it", fi.Name())
	}
	return uint64(st.Nlink), nil //nolint:unconvert // Nlink is uint16 on macOS and uint64 on Linux
}

//go:build unix

package lock

import (
	"strconv"
	"syscall"
)

// umaskTake takes path's lock under the octal umask mask.
func umaskTake(mask, path string) error {
	m, err := strconv.ParseUint(mask, 8, 32)
	if err != nil {
		return err
	}
	syscall.Umask(int(m))
	l, err := Take(path, path+".holder")
	if err != nil {
		return err
	}
	return l.Release()
}

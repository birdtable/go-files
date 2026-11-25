//go:build windows

package appdirs

import (
	"io/fs"

	"golang.org/x/sys/windows"
)

// Free is the bytes free to this account, under its quota, on the volume that holds path, a
// folder. A path that is not there is an *fs.PathError matching fs.ErrNotExist.
func Free(path string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(path)
	var free uint64
	if err == nil {
		err = windows.GetDiskFreeSpaceEx(p, &free, nil, nil)
	}
	if err != nil {
		return 0, &fs.PathError{Op: opFree, Path: path, Err: err}
	}
	return free, nil
}

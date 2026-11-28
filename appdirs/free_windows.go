//go:build windows

package appdirs

import (
	"errors"
	"io/fs"
	"strings"

	"golang.org/x/sys/windows"

	"github.com/birdtable/go-files/internal/winapi"
)

// Free is the bytes free to this account, under its quota, on the volume that holds path, a
// folder. A path that is not there is an *fs.PathError matching fs.ErrNotExist. path is opened by
// winapi.OpenPath, its links followed, and must be a folder on a volume; the system is then asked
// of that volume's root, \\?\Volume{GUID}\, or, for a folder on a share, of the folder's final
// path, never of path again, so a junction on path's way leads its question nowhere but the open
// that asks for identification alone.
func Free(path string) (uint64, error) {
	if err := notDevice(opFree, path); err != nil {
		return 0, err
	}
	name, err := freeName(path)
	var free uint64
	if err == nil {
		err = windows.GetDiskFreeSpaceEx(name, &free, nil, nil)
	}
	if err != nil {
		return 0, &fs.PathError{Op: opFree, Path: path, Err: err}
	}
	return free, nil
}

// freeName is the name Free asks GetDiskFreeSpaceEx of for path: the root of the volume the
// folder open at path is on, or, where the volume has no GUID, as a share has none, the folder's
// final path, ending in a separator. A path that is no folder on a volume is refused, a file with
// ERROR_DIRECTORY as GetDiskFreeSpaceEx refuses one.
func freeName(path string) (*uint16, error) {
	f, err := winapi.OpenPath(path, true)
	if err != nil {
		if pe, ok := errors.AsType[*fs.PathError](err); ok {
			return nil, pe.Err
		}
		return nil, err
	}
	defer f.Close() //nolint:errcheck // a handle only queried
	h := windows.Handle(f.Fd())
	if err := onVolume(h); err != nil {
		return nil, err
	}
	if info, err := f.Stat(); err != nil || !info.IsDir() {
		return nil, errors.Join(err, windows.ERROR_DIRECTORY)
	}
	if root, err := volumeRoot(h); err == nil {
		return root, nil
	}
	dos, err := finalPath(h)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(dos, `\`) {
		dos += `\`
	}
	return windows.UTF16PtrFromString(dos)
}

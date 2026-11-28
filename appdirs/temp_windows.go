//go:build windows

package appdirs

import (
	"io/fs"
	"strings"

	"golang.org/x/sys/windows"

	"github.com/birdtable/go-files/internal/winapi"
)

// tempName is the folder of app under the account's own temp folder.
func tempName(app string) string { return app }

// realPath is the path of what p names, every link and junction on its way resolved: the name is
// opened through its reparse points, and the system names the file the handle is open on.
// filepath.EvalSymlinks does not serve: since Go 1.23 os reads a junction as no link, so it keeps one.
func realPath(p string) (string, error) {
	name, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", &fs.PathError{Op: "open", Path: p, Err: err}
	}
	h, err := winapi.CreateFile(name, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS)
	if err != nil {
		return "", &fs.PathError{Op: "open", Path: p, Err: err}
	}
	got, err := finalPath(h)
	if cerr := windows.CloseHandle(h); cerr != nil && err == nil {
		return "", &fs.PathError{Op: "close", Path: p, Err: cerr}
	}
	if err != nil {
		return "", &fs.PathError{Op: "GetFinalPathNameByHandle", Path: p, Err: err}
	}
	return got, nil
}

// finalPath is the system's path of the file open at h, with its drive letter (flags 0:
// FILE_NAME_NORMALIZED and VOLUME_NAME_DOS), its buffer grown to the length the system asks for.
func finalPath(h windows.Handle) (string, error) {
	size := uint32(windows.MAX_PATH)
	for {
		buf := make([]uint16, size)
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], size, 0)
		if err != nil {
			return "", err
		}
		if n < size {
			return dosPath(windows.UTF16ToString(buf[:n])), nil
		}
		size = n
	}
}

// dosPath is a final path without the \\?\ prefix the system gives it: \\?\C:\x is C:\x, and
// \\?\UNC\host\share\x is \\host\share\x.
func dosPath(p string) string {
	if rest, ok := strings.CutPrefix(p, `\\?\UNC\`); ok {
		return `\\` + rest
	}
	return strings.TrimPrefix(p, `\\?\`)
}

// newFolder makes the temp folder dir with the owner-only access list (winapi.MakeFolder), which what
// is made in it inherits. A volume that keeps no access lists is refused with ErrNoAccessList.
// listless takes every volume as one (temp).
func newFolder(dir string, listless bool) error {
	return winapi.MakeFolder(winapi.OpenFolder, dir, winapi.Listless{Err: ErrNoAccessList, Test: listless})
}

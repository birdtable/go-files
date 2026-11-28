package confine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"

	"github.com/birdtable/go-files/internal/winapi"
	"golang.org/x/sys/windows"
)

// finalHeld refuses nothing on Windows: a file with a second name answers the name it was opened by.
func finalHeld(fs.FileInfo) error { return nil }

// finalVolumes refuses no volume on Windows: a folder has no second name there.
func finalVolumes(*os.File, string) error { return nil }

// openFinal opens path for its attributes alone, sharing all, with FILE_FLAG_BACKUP_SEMANTICS so
// a folder opens as a file does and FILE_FLAG_OPEN_REPARSE_POINT so a link or junction at the last
// name is opened itself, by winapi.CreateFile, which asks for identification alone, so a pipe a
// link on the way leads to learns who the caller is and may act as it in nothing. What opens as
// any file type but a disk's, a pipe or a device a link on the way leads to, is no folder or file:
// it is closed and refused, matching fs.ErrInvalid, before any call asks it its name.
func openFinal(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := winapi.CreateFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(h), path) // owns h, and closes it
	t, err := windows.GetFileType(h)
	if err == nil && t != windows.FILE_TYPE_DISK {
		err = fmt.Errorf("it opened as file type %d, a pipe or a device, no folder or file: %w", t, fs.ErrInvalid)
	}
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return f, nil
}

// finalOf asks GetFinalPathNameByHandle for f's normalized name with its drive, then drops the
// \\?\ prefix where undevice says the name reads the same without it.
func finalOf(f *os.File) (string, error) {
	const normalizedDOS = 0x0 // FILE_NAME_NORMALIZED | VOLUME_NAME_DOS, from fileapi.h
	size := uint32(windows.MAX_PATH)
	for {
		buf := make([]uint16, size)
		n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buf[0], size, normalizedDOS)
		if err != nil {
			return "", err
		}
		if n < size {
			return undevice(windows.UTF16ToString(buf[:n]), syscall.FullPath), nil
		}
		size = n
	}
}

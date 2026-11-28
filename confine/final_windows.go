package confine

import (
	"io/fs"
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// finalHeld refuses nothing on Windows: a file with a second name answers the name it was opened by.
func finalHeld(fs.FileInfo) error { return nil }

// finalVolumes refuses no volume on Windows: a folder has no second name there.
func finalVolumes(*os.File, string) error { return nil }

// openFinal opens path for its attributes alone, sharing all, with FILE_FLAG_BACKUP_SEMANTICS so
// a folder opens as a file does and FILE_FLAG_OPEN_REPARSE_POINT so a link or junction at the last
// name is opened itself.
func openFinal(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil // owns h, and closes it
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

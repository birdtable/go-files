package appdirs

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"

	"github.com/birdtable/go-files/internal/fileid"
	"github.com/birdtable/go-files/internal/winapi"
)

// indexAtPath opens path for its file index alone, following its links (onWay compares the resolved
// path by file ID), with FILE_READ_ATTRIBUTES and FILE_FLAG_BACKUP_SEMANTICS so a folder opens as a
// file does. It checks the opened file is the one the caller looked at, its Stat against held, and
// refuses `changed` on a mismatch, then refuses a no-index volume with fileid.ErrNoFileIndex (one value
// with write.ErrNoFileIndex, so errors.Is holds with it): a temp folder on a WebDAV or SMB drive or on
// ReFS, where os.SameFile cannot tell two folders apart, is not read as holding or not holding one it
// is kept apart from.
func indexAtPath(path string, held os.FileInfo, name string, changed error) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	h, err := winapi.CreateFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(h), path) // owns h, and closes it
	defer f.Close()                   //nolint:errcheck // read only; the file index was already read
	got, err := f.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(got, held) {
		return changed
	}
	switch ok, ierr := fileid.Indexed(f, false); {
	case ierr != nil:
		return ierr
	case !ok:
		return fmt.Errorf("%s: %w", name, fileid.ErrNoFileIndex)
	}
	return nil
}

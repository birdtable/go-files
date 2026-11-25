package lock

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/birdtable/go-files/internal/fileid"
	"github.com/birdtable/go-files/internal/winapi"
	"golang.org/x/sys/windows"
)

const (
	noFollow   = syscall.FILE_FLAG_OPEN_REPARSE_POINT // a link opens as itself; openLock refuses it
	noBlock    = 0
	errSharing = syscall.Errno(32) // ERROR_SHARING_VIOLATION
)

func unlinked(at in, path string, f *os.File) error { return atName(at, path, f) } // a handle at a link opens the link

// tryLock takes LockFileEx's lock on the first byte of f, the file openLock opened and checked,
// and is false while another holds it.
func tryLock(f *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, new(windows.Overlapped))
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}

func unlock(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
}

func openErr(err error) error { return err }

func retried(op func() error) error { return winapi.Retried(op) }

func inUse(err error) bool { return errors.Is(err, errSharing) }

func owned(*os.File, os.FileInfo, bool) error { return nil }

// newLock makes the lock file at path by an exclusive create that follows no link, to read and
// write, with the owner-only access list (winapi.MakeFile), sharing read and write alone, as the
// open of a lock file there does (openLock): while it is held, no process removes or renames it,
// so no later Take makes a second lock at the name. A volume that keeps no access lists is refused
// with ErrNoAccessList, and a last name ending in a dot or a space with winapi.ErrStrippedName,
// matching fs.ErrInvalid; listless takes every volume as one (acquire).
func newLock(at in, path string, listless bool) (*os.File, error) {
	open := os.Open
	if at.root != nil {
		open = at.root.Open
	}
	return winapi.MakeFile(open, path, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, winapi.Listless{Err: ErrNoAccessList, Test: listless})
}

// indexAt refuses name on a Windows volume that gives the lock file f no 64-bit index os.SameFile can
// tell it apart from another by (fileid.ErrNoFileIndex, matching write.ErrNoFileIndex): a WebDAV or SMB
// drive, whose files read a zero index, or ReFS, whose index comes back all-ones. Its errors.Is holds
// with write.ErrNoFileIndex. The note or path a compare looks at sits in the lock file's own folder, so
// one read of f's index covers the compare that follows.
func indexAt(f *os.File, name string) error {
	switch ok, err := fileid.Indexed(f, false); {
	case err != nil:
		return err
	case !ok:
		return fmt.Errorf("%s: %w", name, fileid.ErrNoFileIndex)
	}
	return nil
}

// open opens name as flag says, to read or to read and write, sharing read and write as os does,
// with a link at its last element opened as itself, which unlinked then refuses: by path, or below
// at's root in name's folder, opened through the root.
func (at in) open(name string, flag int, _ fs.FileMode) (*os.File, error) {
	if at.root == nil {
		return os.OpenFile(name, flag|noFollow, 0) //nolint:gosec // the caller's lock or note path, a link opened as itself
	}
	d, err := at.root.Open(filepath.Dir(name))
	if err != nil {
		return nil, err
	}
	defer d.Close() //nolint:errcheck // a folder opened only to open in; its close loses nothing
	access := uint32(windows.FILE_GENERIC_READ)
	if flag&os.O_RDWR != 0 {
		access |= windows.FILE_GENERIC_WRITE
	}
	h, err := winapi.OpenAt(windows.Handle(d.Fd()), filepath.Base(name), winapi.How{Access: access,
		Share: windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE, Disposition: windows.FILE_OPEN, Attributes: windows.OBJ_CASE_INSENSITIVE})
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: winapi.Dos(err)}
	}
	return os.NewFile(uintptr(h), name), nil
}

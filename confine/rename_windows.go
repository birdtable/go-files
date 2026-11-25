package confine

// The move that never replaces, on Windows: by handle, inside the root. The README says how.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/birdtable/go-files/internal/winapi"
)

const stepHeld = "between the hold and the rename"

const stripsTrail, swaps = true, false

// rename holds each folder of both names, from the root down, with no share to delete, so none
// can be swapped for a junction, and moves the source with NtSetInformationFile, ReplaceIfExists
// false. SetFileInformationByHandle refuses a RootDirectory on Windows 11 26200.
func rename(root *os.Root, from, to string, _ bool) error {
	return renameHeld(root, from, to, func(string) {})
}

func renameHeld(root *os.Root, from, to string, reached func(step string)) (err error) {
	top, err := root.Open(".")
	if err != nil {
		return err
	}
	defer closing(top, &err)
	src, err := hold(windows.Handle(top.Fd()), from, opRename)
	if err != nil {
		return err
	}
	defer closing(src, &err)
	dst, err := hold(windows.Handle(top.Fd()), to, opRename)
	if err != nil {
		return err
	}
	defer closing(dst, &err)
	if err := missing(dst.in, to); err != nil {
		return err
	}
	f, err := source(src.in, from)
	if err != nil {
		return err
	}
	defer closing(f, &err)
	reached(stepHeld)
	return moveTo(f.h, dst.in, from, to)
}

// held is the folders of a name's path below the root, and in, the one its last component is in:
// the root's own handle when there is none below it.
type held struct {
	folders []windows.Handle
	in      windows.Handle
}

// Close closes the held folders.
func (h *held) Close() error {
	var err error
	for _, f := range h.folders {
		err = errors.Join(err, windows.CloseHandle(f))
	}
	return err
}

// hold opens each folder of name's path from the one before it, top first, and refuses a reparse
// point, judged on the handle opened. A refusal is an *fs.PathError of op.
func hold(top windows.Handle, name, op string) (*held, error) {
	parts := strings.Split(name, string(filepath.Separator))
	h := &held{in: top}
	for i, part := range parts[:len(parts)-1] {
		at := filepath.ToSlash(filepath.Join(parts[:i+1]...))
		f, err := ntOpen(h.in, part, at, op, winapi.How{Access: windows.FILE_LIST_DIRECTORY | windows.FILE_TRAVERSE, Share: windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE, Disposition: windows.FILE_OPEN})
		if err == nil {
			err = f.folder(op, at)
		}
		if err != nil {
			if f != nil {
				err = errors.Join(err, f.Close())
			}
			return nil, errors.Join(err, h.Close())
		}
		h.folders = append(h.folders, f.h)
		h.in = f.h
	}
	return h, nil
}

// missing refuses to when its last component is in in: a reparse point or hard link with
// ErrOutside, anything else with fs.ErrExist. The move refuses a name made since.
func missing(in windows.Handle, to string) error {
	f, err := openAt(in, filepath.Base(to), filepath.ToSlash(to), 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := f.last(to); err != nil {
		return errors.Join(err, f.Close())
	}
	return errors.Join(taken(to, false), f.Close())
}

// source opens from's last component in in to be renamed, with no share to delete, and refuses a
// reparse point and a hard link.
func source(in windows.Handle, from string) (*handle, error) {
	f, err := openAt(in, filepath.Base(from), filepath.ToSlash(from), windows.DELETE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
	if err != nil {
		return nil, err
	}
	if err := f.last(from); err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return f, nil
}

type handle struct {
	h    windows.Handle
	info windows.ByHandleFileInformation
}

// Close closes the handle.
func (f *handle) Close() error { return windows.CloseHandle(f.h) }

func (f *handle) reparse() bool {
	return f.info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func (f *handle) folder(op, at string) error {
	switch {
	case f.reparse():
		return &fs.PathError{Op: op, Path: at, Err: fmt.Errorf("it is a junction or another reparse point, and no link is followed: %w", ErrOutside)}
	case f.info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0:
		return &fs.PathError{Op: op, Path: at, Err: errors.New("it is a file, not a folder")}
	}
	return nil
}

// last refuses a reparse point, and a file with a second name. A folder's count is not read.
func (f *handle) last(name string) error {
	switch {
	case f.reparse():
		return lastRefused(name, "a junction, a link or another reparse point")
	case f.info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 && f.info.NumberOfLinks > 1:
		return lastRefused(name, "a file with another name, a hard link")
	}
	return nil
}

// openAt opens part in dir with access and share, following no reparse point, and reads what the
// handle says it is. A missing name matches fs.ErrNotExist.
func openAt(dir windows.Handle, part, at string, access, share uint32) (*handle, error) {
	return ntOpen(dir, part, at, opRename, winapi.How{Access: access, Share: share, Disposition: windows.FILE_OPEN})
}

// ntOpen opens part in dir as how says, following no reparse point, and reads
// what the handle says it is. A refusal is an *fs.PathError of op naming at; a missing name
// matches fs.ErrNotExist, and with FILE_CREATE a name there matches fs.ErrExist. A folder there, a
// link to one among them, at a make of a file is answered STATUS_FILE_IS_A_DIRECTORY before the
// system looks for a collision, and that status's Win32 error is ERROR_ACCESS_DENIED, so under
// FILE_CREATE it is read as the collision it is.
func ntOpen(dir windows.Handle, part, at, op string, how winapi.How) (*handle, error) {
	how.Access |= windows.FILE_READ_ATTRIBUTES
	h, err := winapi.OpenAt(dir, part, how)
	if how.Disposition == windows.FILE_CREATE && errors.Is(err, windows.STATUS_FILE_IS_A_DIRECTORY) {
		err = windows.STATUS_OBJECT_NAME_COLLISION
	}
	if err = winapi.Dos(err); err != nil {
		return nil, &fs.PathError{Op: op, Path: at, Err: err}
	}
	f := &handle{h: h}
	if err := windows.GetFileInformationByHandle(f.h, &f.info); err != nil {
		return nil, errors.Join(&fs.PathError{Op: op, Path: at, Err: err}, f.Close())
	}
	return f, nil
}

func moveTo(src, in windows.Handle, from, to string) error {
	err := winapi.Rename(src, in, filepath.Base(to), false)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrExist):
		return &fs.PathError{Op: opRename, Path: filepath.ToSlash(to), Err: fs.ErrExist}
	case errors.Is(err, windows.ERROR_FILENAME_EXCED_RANGE), errors.Is(err, syscall.EINVAL): // to's name too long, or with a NUL
		return &fs.PathError{Op: opRename, Path: filepath.ToSlash(to), Err: err}
	}
	return &fs.PathError{Op: opRename, Path: filepath.ToSlash(from), Err: err}
}

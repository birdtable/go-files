//go:build linux || darwin

package confine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// sysOpen is the kernel's open of name beneath dirfd with no link followed. A test passes another.
type sysOpen func(dirfd int, name string, flag int, mode uint32) (int, error)

func open(root *os.Root, name string, flag int, perm fs.FileMode, reached func(step string)) (*os.File, error) {
	if f, handled, err := sysOpen(kernelOpen).open(root, name, flag, perm, reached); handled {
		return f, err
	}
	return walkOpen(root, name, flag, perm, reached)
}

// open opens name in root through call, and answers false, having opened nothing, only on ENOSYS.
func (call sysOpen) open(root *os.Root, name string, flag int, perm fs.FileMode, reached func(step string)) (*os.File, bool, error) {
	asked := flag &^ os.O_TRUNC
	if asked < 0 {
		return nil, true, &fs.PathError{Op: opOpen, Path: filepath.ToSlash(name), Err: fmt.Errorf("the open flags %#x are none a file is opened with", flag)}
	}
	dir, err := root.Open(".")
	if err != nil {
		return nil, true, err
	}
	var mode uint32
	if flag&os.O_CREATE != 0 {
		mode = syscallMode(perm)
	}
	reached(stepOpen)
	fd, err := call(int(dir.Fd()), name, asked|unix.O_CLOEXEC, mode)
	for errors.Is(err, unix.EINTR) {
		fd, err = call(int(dir.Fd()), name, asked|unix.O_CLOEXEC, mode)
	}
	closed := dir.Close()
	switch {
	case errors.Is(err, unix.ENOSYS):
		return nil, closed != nil, closed
	case err != nil:
		return nil, true, errors.Join(refusal(root, name, err), closed)
	}
	f := os.NewFile(uintptr(fd), filepath.Join(root.Name(), name))
	if err := errors.Join(closed, namesOf(f, name, flag)); err != nil {
		return nil, true, errors.Join(err, f.Close())
	}
	if flag&os.O_TRUNC != 0 {
		if err := f.Truncate(0); err != nil {
			return nil, true, errors.Join(err, f.Close())
		}
	}
	return f, true, nil
}

// refusal is the error for the kernel's answer err. A walk names a link or a component that is
// not a folder, after the kernel refused, and opens nothing. ELOOP, EXDEV, and an ENOTDIR where
// the walk finds a folder or nothing (a swap in between), match ErrOutside.
func refusal(root *os.Root, name string, err error) error {
	errno, _ := errors.AsType[unix.Errno](err)
	var walked fs.FileInfo
	if errno != unix.EXDEV {
		info, err := NoLinks(root, name)
		if err != nil {
			return err
		}
		walked = info
	}
	switch {
	case errno == unix.ELOOP:
		return &fs.PathError{Op: opWalk, Path: filepath.ToSlash(name),
			Err: fmt.Errorf("a component is a link, and no link is followed: %w", ErrOutside)}
	case errno == unix.EXDEV:
		return &fs.PathError{Op: opWalk, Path: filepath.ToSlash(name), Err: fmt.Errorf("it leads outside the folder: %w", ErrOutside)}
	case errno == unix.ENOTDIR && (walked == nil || walked.IsDir()):
		return &fs.PathError{Op: opWalk, Path: filepath.ToSlash(name),
			Err: fmt.Errorf("a component changed while the kernel resolved it: %w", ErrOutside)}
	}
	return &fs.PathError{Op: opOpen, Path: filepath.ToSlash(name), Err: err}
}

// syscallMode is perm as os.OpenFile passes it to open.
func syscallMode(perm fs.FileMode) uint32 {
	m := uint32(perm.Perm())
	if perm&fs.ModeSetuid != 0 {
		m |= unix.S_ISUID
	}
	if perm&fs.ModeSetgid != 0 {
		m |= unix.S_ISGID
	}
	if perm&fs.ModeSticky != 0 {
		m |= unix.S_ISVTX
	}
	return m
}

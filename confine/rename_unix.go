//go:build linux || darwin

package confine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const stripsTrail, swaps = false, true

// rename acts through descriptors of the two folders, each opened by Open's kernel call, and looks
// at each last component from them; the system's call follows no link at either.
func rename(root *os.Root, from, to string, exchange bool) (err error) {
	src, err := folderOf(root, from)
	if err != nil {
		return err
	}
	defer closing(src, &err)
	dst, err := folderOf(root, to)
	if err != nil {
		return err
	}
	defer closing(dst, &err)
	there, err := last(src, from)
	if err == nil && !there {
		err = &fs.PathError{Op: opRename, Path: filepath.ToSlash(from), Err: fs.ErrNotExist}
	}
	if err != nil {
		return err
	}
	if there, err = last(dst, to); err != nil {
		return err
	}
	if there != exchange {
		return taken(to, exchange)
	}
	if within(to, from) || exchange && within(from, to) {
		// the kernel's answer, given before the call, so the EINVAL a call returns is the file system's
		return &os.LinkError{Op: opRename, Old: from, New: to, Err: unix.EINVAL}
	}
	err = renameAt(int(src.Fd()), filepath.Base(from), int(dst.Fd()), filepath.Base(to), exchange)
	return renamed(err, from, to, exchange)
}

// within is whether the clean name is dir or lies inside it: a folder moved into itself, or
// swapped with its own child or parent. Neither name holds a link, so the names alone decide.
func within(name, dir string) bool {
	return name == dir || strings.HasPrefix(name, dir+string(filepath.Separator))
}

func folderOf(root *os.Root, name string) (*os.File, error) {
	return open(root, filepath.Dir(name), os.O_RDONLY|unix.O_DIRECTORY, 0, func(string) {})
}

// last is whether name's last component is in dir. It refuses a link, and a file with a second
// name, which os.Root's Rename takes. A folder's count of names is its own, and is not read.
func last(dir *os.File, name string) (bool, error) {
	var st unix.Stat_t
	err := unix.Fstatat(int(dir.Fd()), filepath.Base(name), &st, unix.AT_SYMLINK_NOFOLLOW)
	switch {
	case errors.Is(err, unix.ENOENT):
		return false, nil
	case err != nil:
		return false, &fs.PathError{Op: opRename, Path: filepath.ToSlash(name), Err: err}
	case st.Mode&unix.S_IFMT == unix.S_IFLNK:
		return false, lastRefused(name, "a link")
	case st.Mode&unix.S_IFMT == unix.S_IFREG && st.Nlink > 1:
		return false, lastRefused(name, "a file with another name, a hard link")
	}
	return true, nil
}

// renamed is the error for the system's answer err: a taken name is fs.ErrExist, and a flag the
// kernel or file system does not take is ErrNoExchange for a swap, errors.ErrUnsupported for a
// move.
func renamed(err error, from, to string, exchange bool) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, unix.EEXIST):
		return &fs.PathError{Op: opRename, Path: filepath.ToSlash(to), Err: fs.ErrExist}
	case unsupported(err) && exchange:
		return &fs.PathError{Op: opRename, Path: filepath.ToSlash(from), Err: fmt.Errorf("%w (%w)", ErrNoExchange, err)}
	case unsupported(err):
		return &fs.PathError{Op: opRename, Path: filepath.ToSlash(from),
			Err: fmt.Errorf("this kernel or file system cannot rename without replacing in one step, and nothing was moved: %w (%w)", errors.ErrUnsupported, err)}
	}
	return &fs.PathError{Op: opRename, Path: filepath.ToSlash(from), Err: err}
}

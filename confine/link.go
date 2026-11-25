package confine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const opLink = "link"

// LinkFolder makes at, in root, a link to the folder target, one call on every system. On Linux,
// macOS and FreeBSD it is a symbolic link, made by symlinkat from at's folder, opened with no
// link followed. On Windows it is a junction, which needs no privilege: at's folders are held
// open by handle from the root's own, none a junction or another reparse point, the new empty
// folder is made in the last of them, and the mount point is set on that folder's own handle. A
// folder left half made, when the system refuses the mount point, is removed by that handle.
//
// It refuses, with an error matching ErrOutside, an at that is not a clean name below the root
// (the root itself among them), a link or junction at any component of at's folder, and on
// Windows a component that ends in a dot or a space. A name already at at, a link included, is
// fs.ErrExist, and nothing is replaced or followed. A target that is not a clean absolute path
// to a folder there now is refused with an *os.LinkError matching fs.ErrInvalid, or the stat's
// own error when nothing is there. On Windows so is a target that is not on a drive letter of a
// fixed or removable disk of this machine (a share, a letter mapped to one, and a letter subst
// made are refused), one with a component that ends in a dot or a space, and one whose mount
// point data would not fit. A refusal makes nothing. On NetBSD and
// every other system it refuses with errors.ErrUnsupported.
func LinkFolder(root *os.Root, target, at string) error {
	return linkChecked(root, target, at, true)
}

// LinkFile makes at, in root, a symbolic link to the file target, under LinkFolder's refusals of
// at, and never a hard link or a copy. On Linux, macOS and FreeBSD it is made as LinkFolder makes
// one. On Windows the file at is made new, by handle, in at's folders held as LinkFolder holds
// them, and the link is set on that file's own handle, which takes the privilege to make a
// symbolic link; where this account does not hold it, the file made is removed by that handle and
// it refuses with ErrNoFileLink. NetBSD and every other system refuse with ErrNoFileLink.
//
// A target that is not a clean absolute path to a regular file there now, a folder among them, is
// refused with an *os.LinkError matching fs.ErrInvalid, or the stat's own error when nothing is
// there, and on Windows so is a target LinkFolder refuses for its drive or its name. A refusal
// makes nothing.
func LinkFile(root *os.Root, target, at string) error {
	return linkChecked(root, target, at, false)
}

// linkChecked makes at a link to target, a folder or a file, past LinkFolder's checks.
func linkChecked(root *os.Root, target, at string, folder bool) error {
	return linkAs(root, target, target, at, folder)
}

// linkAs is linkChecked, the link holding text, which the caller makes name target from at's
// folder: target is what is checked.
func linkAs(root *os.Root, target, text, at string, folder bool) error {
	if err := clean(at); err != nil || at == "." {
		return &fs.PathError{Op: opLink, Path: filepath.ToSlash(at), Err: fmt.Errorf("it is not a clean name below the root: %w", ErrOutside)}
	}
	if stripsTrail && trailed(at, string(filepath.Separator)) {
		return &fs.PathError{Op: opLink, Path: filepath.ToSlash(at), Err: fmt.Errorf("a component of it ends in a dot or a space, which the Win32 name rules strip, so it would name one file to os.Root and another to the link: %w", ErrOutside)}
	}
	if err := linkTarget(target, folder); err != nil {
		return &os.LinkError{Op: opLink, Old: target, New: at, Err: err}
	}
	if folder {
		return link(root, text, at)
	}
	return linkFile(root, text, at)
}

// linkTarget refuses target unless it is clean, absolute, and now a folder, or a regular file
// where folder is false. It is judged as written, and the link holds it as written.
func linkTarget(target string, folder bool) error {
	if !filepath.IsAbs(target) || filepath.Clean(target) != target {
		return fmt.Errorf("the target is not a clean absolute path: %w", fs.ErrInvalid)
	}
	if err := fits(target); err != nil {
		return err
	}
	st, err := os.Stat(target)
	if err != nil {
		return err
	}
	if folder && !st.IsDir() {
		return fmt.Errorf("the target is not a folder, and a link here is to a folder only: %w", fs.ErrInvalid)
	}
	if !folder && !st.Mode().IsRegular() {
		return fmt.Errorf("the target is not a regular file, and a link here is to a file only: %w", fs.ErrInvalid)
	}
	return nil
}

// madeErr is the error for the system's answer err to the make of at's last component: a name
// there is fs.ErrExist.
func madeErr(at string, err error) error {
	if errors.Is(err, fs.ErrExist) {
		return &fs.PathError{Op: opLink, Path: filepath.ToSlash(at), Err: fs.ErrExist}
	}
	return &fs.PathError{Op: opLink, Path: filepath.ToSlash(at), Err: err}
}

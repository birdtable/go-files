package confine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const opCollide = "collide"

// Collide answers whether the folder root reads a and b, two names in it, as one file: whether
// the file at one is found at the other. It answers for the volume and the folder, not for the
// system: APFS as macOS formats it folds case and takes é composed and decomposed for one name,
// NTFS folds case alone, ext4 folds neither, and a folder may be set otherwise, as ext4's casefold
// or NTFS's case sensitivity per folder. a == b is true, with nothing looked at.
//
// Each name is looked at with no link followed. Both there and the same file is true, both there
// and two files is false, and one there with the other not is false. Where neither is there, a is
// made, empty and the owner's alone, b is looked at, and a is removed again. The probe is held open
// until after the remove, so no file put at a or b takes its file ID; a file put at a by another is
// left only up to the last instant: no call removes by file ID, so a look then a remove.
//
// It refuses, with an *fs.PathError matching ErrOutside, a name that is not one name in the
// folder: empty, ., .., or one holding a separator.
func Collide(root *os.Root, a, b string) (bool, error) {
	return collide(root, a, b, func() {})
}

func collide(root *os.Root, a, b string, made func()) (bool, error) {
	for _, name := range []string{a, b} {
		if !filepath.IsLocal(name) || name == "." || strings.ContainsAny(name, "/"+string(filepath.Separator)) {
			return false, &fs.PathError{Op: opCollide, Path: filepath.ToSlash(name), Err: fmt.Errorf("it is not one name in the folder: %w", ErrOutside)}
		}
	}
	if a == b {
		return true, nil
	}
	fa, err := lstatThere(root, a)
	if err != nil {
		return false, err
	}
	fb, err := lstatThere(root, b)
	switch {
	case err != nil:
		return false, err
	case fa != nil && fb != nil:
		if ierr := indexSame(root, a, fa); ierr != nil {
			return false, ierr
		}
		if ierr := indexSame(root, b, fb); ierr != nil {
			return false, ierr
		}
		return os.SameFile(fa, fb), nil
	case fa != nil || fb != nil:
		return false, nil
	}
	f, err := root.OpenFile(a, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return false, err
	}
	return probed(root, f, a, b, made)
}

// probed looks at b with the probe f, made at a, held open, and removes a when the file there is
// still the probe. The probe is held until after the remove: a held file keeps its file ID, so a
// file put at a or b once the probe is gone cannot take the ID ext4 hands out again at once and
// pass for the probe. os.Root opens with FILE_SHARE_DELETE on Windows, so the
// remove of a held file goes through there too.
func probed(root *os.Root, f *os.File, a, b string, made func()) (same bool, err error) {
	defer func() { err = errors.Join(err, f.Close()) }()
	mine, err := f.Stat()
	if err != nil {
		return false, fmt.Errorf("the probe %s is left in the folder: %w", a, err)
	}
	if err := indexHeld(f, a); err != nil {
		return false, err
	}
	made()
	fb, err := lstatThere(root, b)
	if now, lerr := root.Lstat(a); lerr == nil && os.SameFile(mine, now) {
		err = errors.Join(err, root.Remove(a))
	}
	return fb != nil && os.SameFile(mine, fb), err
}

// lstatThere is root.Lstat of name, or nil with no error where nothing is there.
func lstatThere(root *os.Root, name string) (fs.FileInfo, error) {
	fi, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return fi, err
}

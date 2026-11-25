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

// collideTries is how many times Collide looks at a and b before it takes a folder that changes
// under every look for one that reads them as one.
const collideTries = 3

// The looks collide makes, named to its looked hook: the first at a, the first at b, and the second
// at a and at b before an answer of apart.
const (
	lookedA      = "a"
	lookedB      = "b"
	lookedAAgain = "a again"
	lookedBAgain = "b again"
)

// Collide answers whether the folder root reads a and b, two names in it, as one file: whether
// the file at one is found at the other. It answers for the volume and the folder, not for the
// system: APFS as macOS formats it folds case and takes é composed and decomposed for one name,
// NTFS folds case alone, ext4 folds neither, and a folder may be set otherwise, as ext4's casefold
// or NTFS's case sensitivity per folder. a == b is true, with nothing looked at.
//
// Each name is looked at with no link followed. Both there and the same file is true, both there
// and two files is false, and one there with the other not is false. An answer of false is given
// only when a and b, each looked at again, are what they were: nothing there where nothing was, and
// the same file where one was. A file made or removed at a between the looks would otherwise be
// seen at one name and not at the other on a volume that folds them, another call's probe among
// them, and two names that are one be answered apart. Where a look changed, it looks again from the
// start, as it does where its probe finds a made since; where the folder changes under each of
// three tries, it answers true, taking names it cannot tell apart for one. Where neither is there,
// a is made, empty and the owner's alone, b is looked at, and a is removed again. The probe is held
// open until after the remove, so no file put at a or b takes its file ID; a file put at a by
// another is left only up to the last instant: no call removes by file ID, so a look then a remove.
//
// It refuses, with an *fs.PathError matching ErrOutside, a name that is not one name in the
// folder: empty, ., .., or one holding a separator.
func Collide(root *os.Root, a, b string) (bool, error) {
	return collide(root, a, b, func() {}, func(string) {})
}

func collide(root *os.Root, a, b string, made func(), looked func(step string)) (bool, error) {
	for _, name := range []string{a, b} {
		if !filepath.IsLocal(name) || name == "." || strings.ContainsAny(name, "/"+string(filepath.Separator)) {
			return false, &fs.PathError{Op: opCollide, Path: filepath.ToSlash(name), Err: fmt.Errorf("it is not one name in the folder: %w", ErrOutside)}
		}
	}
	if a == b {
		return true, nil
	}
	for range collideTries {
		fa, err := collideLook(root, a, lookedA, looked)
		if err != nil {
			return false, err
		}
		fb, err := collideLook(root, b, lookedB, looked)
		if err != nil {
			return false, err
		}
		if fa == nil && fb == nil {
			f, err := root.OpenFile(a, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if errors.Is(err, fs.ErrExist) {
				continue // made at a, or at a name the volume takes for it, since the looks
			}
			if err != nil {
				return false, err
			}
			return probed(root, f, a, b, made)
		}
		if fa != nil && fb != nil {
			ierr := indexSame(root, a, fa)
			if ierr == nil {
				ierr = indexSame(root, b, fb)
			}
			if errors.Is(ierr, fs.ErrNotExist) {
				continue // removed since its look
			}
			if ierr != nil {
				return false, ierr
			}
			if os.SameFile(fa, fb) {
				return true, nil
			}
		}
		// Apart, as far as these looks go: it holds only where neither name changed while looked at.
		fa2, err := collideLook(root, a, lookedAAgain, looked)
		if err != nil {
			return false, err
		}
		fb2, err := collideLook(root, b, lookedBAgain, looked)
		if err != nil {
			return false, err
		}
		if unchanged(fa, fa2) && unchanged(fb, fb2) {
			return false, nil
		}
	}
	return true, nil
}

// collideLook is lstatThere of name, with looked told step once it is looked at.
func collideLook(root *os.Root, name, step string, looked func(string)) (fs.FileInfo, error) {
	fi, err := lstatThere(root, name)
	looked(step)
	return fi, err
}

// unchanged is whether two looks at one name saw the same: nothing at both, or one file at both.
func unchanged(before, after fs.FileInfo) bool {
	if before == nil || after == nil {
		return before == nil && after == nil
	}
	return os.SameFile(before, after)
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

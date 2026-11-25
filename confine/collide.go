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
// and two files is false, and one there with the other not is false, but only where a second look
// at a and at b agrees with the first: a file made or removed at a between the looks, another
// call's probe among them, is seen at one of two names a volume folds and not at the other. Where a
// look changed, it looks again from the start; where the folder changes under three tries, it
// answers true, taking names it cannot tell apart for one. So, raced by other calls, it can answer
// true for two names on a volume that folds neither: 2 to 9 calls in 100, with eight at once on one
// folder. One race is left: a file at a moved away, back and away again within one call, each move
// between two of its four looks, is answered apart, and a new file given the old one's file ID can
// stand in for the move back. Where neither is there, a is made, empty and the owner's alone, b is
// looked at, and a is removed again. The probe is held open until after the remove, so no file put
// at a or b takes its file ID; a file put at a by another is left only up to the last instant: no
// call removes by file ID, so a look then a remove.
//
// It refuses, with an *fs.PathError matching ErrOutside, a name that is not one name in the
// folder: empty, ., .., or one holding a separator.
//
// A caller that may not write, only asks, or must not be swayed to true by other calls' probes
// calls Folds, which reads the folder's rule and makes nothing. Portable's keys are the filter
// before either: a folder that folds takes two names for one only where they share a key.
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
	for range 3 {
		if same, done, err := collideTry(root, a, b, made, looked); done || err != nil {
			return same, err
		}
	}
	return true, nil
}

// collideTry is one try of collide's, not done where what it saw changed under it.
func collideTry(root *os.Root, a, b string, made func(), looked func(string)) (same, done bool, err error) {
	fa, fb, err := lookBoth(root, a, b, "", looked)
	switch {
	case err != nil:
		return false, true, err
	case fa == nil && fb == nil:
		f, err := root.OpenFile(a, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, fs.ErrExist) {
			return false, false, nil // made at a, or at a name folded to it, since the looks
		} else if err != nil {
			return false, true, err
		}
		same, err := probed(root, f, a, b, made)
		return same, true, err
	case fa != nil && fb != nil:
		if err = indexSame(root, a, fa); err == nil {
			err = indexSame(root, b, fb)
		}
		if errors.Is(err, fs.ErrNotExist) {
			return false, false, nil // removed since its look
		} else if err != nil || os.SameFile(fa, fb) {
			return err == nil, true, err
		}
	}
	fa2, fb2, err := lookBoth(root, a, b, " again", looked)
	return false, err != nil || unchanged(fa, fa2) && unchanged(fb, fb2), err
}

// lookBoth is lstatThere of a, then of b, each look told to looked by its name and again.
func lookBoth(root *os.Root, a, b, again string, looked func(string)) (fa, fb fs.FileInfo, err error) {
	if fa, err = lstatThere(root, a); err == nil {
		looked("a" + again)
		fb, err = lstatThere(root, b)
		looked("b" + again)
	}
	return fa, fb, err
}

// unchanged is whether two looks at one name saw the same: nothing at both, or one file at both.
func unchanged(before, after fs.FileInfo) bool {
	return before == nil && after == nil || before != nil && after != nil && os.SameFile(before, after)
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

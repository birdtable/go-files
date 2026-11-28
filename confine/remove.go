package confine

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	opRemove = "remove"

	stepLooked = "between the look at a folder and its open, to empty it"
)

// Remove removes name in root, a file, a link or an empty folder, with no link followed at any
// component before the last. A link or junction at the last is removed as the link itself, and
// what it leads to is left. A name that is not there matches fs.ErrNotExist, and a folder that
// holds anything is refused with the system's own error, and nothing in it is removed.
//
// It refuses, with an error matching ErrOutside, and before anything is removed: a name that is
// not clean and local, as Local gives one, or is the root itself; a link, junction or other
// reparse point at any component before the last; and on Windows a name with a component that
// ends in a dot or a space, as the renames refuse it. The folder the last component is in is
// opened as Open opens it, and held, as a root of its own, to be that folder, so the removal is
// made in it wherever a swap has since moved it.
//
// On Windows a removal refused with a sharing violation or a denial, as a file another process
// holds open answers, is tried again for about 600 ms, the window write.File's rename takes, and
// a file with the read-only attribute is removed. Past the window the system's error is returned
// in an *fs.PathError, and matches fs.ErrPermission. Nothing else here refuses the removal of a
// held file, so it is tried once. The window is a fact of the system, and takes no option.
func Remove(root *os.Root, name string) error {
	return remove(root, name, func(string) {})
}

// remove is Remove, with reached run at each step a test swaps a folder at.
func remove(root *os.Root, name string, reached func(step string)) (err error) {
	in, part, err := folderOfName(root, name, reached)
	if err != nil {
		return err
	}
	defer closing(in, &err)
	return removeName(in, part, filepath.ToSlash(name))
}

// RemoveAll removes name in root and everything beneath it, under Remove's refusals of the name.
// A link or junction met in the tree is removed as the link, never walked into. A folder in the
// tree is opened as the walk opens one, and refused, matching ErrOutside, unless it is the folder
// looked at. A name that is not there is nil, as os.RemoveAll answers. Each name is removed as
// Remove removes one, so on Windows each has its own window. The first error ends it and is
// returned, and what was removed before it stays removed.
func RemoveAll(root *os.Root, name string) error {
	return removeAll(root, name, func(string) {}, noOwner)
}

// RemoveAllOwned is RemoveAll for a tree the caller made and owns, as a run's own home is, that a
// program in it may have left read-only. On Unix each folder of the tree that this account owns,
// by its effective uid, and that lacks its owner's write bit is given it (u+w), by fchmod on the
// folder opened and held to be the folder looked at, before it is emptied; no other bit changes. A
// folder without its owner's search or read bit keeps its mode and is the system's refusal, as
// RemoveAll's is. A folder another account owns keeps its mode and is removed as RemoveAll removes
// it. A link in the tree is never followed and never changed, and a folder swapped in is refused,
// matching ErrOutside, before its mode is touched. On Windows it is RemoveAll, which removes a file
// or folder with the read-only attribute already. The folder name is in is not changed, and the
// first error ends it, leaving each folder given the bit before it with it.
func RemoveAllOwned(root *os.Root, name string) error {
	return removeAll(root, name, func(string) {}, os.Geteuid())
}

// noOwner is the owner RemoveAll changes the mode of no folder for.
const noOwner = -1

// removeAll is RemoveAll, with reached run at each step a test swaps a folder at, and the write bit
// given to each folder owner owns that lacks it (none for noOwner).
func removeAll(root *os.Root, name string, reached func(step string), owner int) (err error) {
	in, part, err := folderOfName(root, name, reached)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer closing(in, &err)
	return removeTree(in, part, filepath.ToSlash(name), reached, owner)
}

// closing closes c into err, joined to what err holds. It is built on every system, as the
// removes are.
func closing(c io.Closer, err *error) { *err = errors.Join(*err, c.Close()) }

// folderOfName refuses name as Remove's doc says, and answers the folder its last component is in,
// opened by into as Mkdir opens one, and that component.
func folderOfName(root *os.Root, name string, reached func(step string)) (*os.Root, string, error) {
	if err := refusedName(opRemove, name); err != nil {
		return nil, "", err
	}
	if err := rootOnPipes(root, name); err != nil && root == nil {
		return nil, "", err
	}
	dir := filepath.Dir(name)
	if dir == "." {
		in, err := root.OpenRoot(".")
		return in, name, err
	}
	in, err := into(root, dir, filepath.ToSlash(dir), false, walk{reached: reached})
	return in, filepath.Base(name), err
}

// removeName removes part in dir, by os.Root's Remove, which follows no link at a last component;
// on Windows it is tried again as Remove's doc says. A refusal is an *fs.PathError naming at.
func removeName(dir *os.Root, part, at string) error {
	err := retried(func() error { return dir.Remove(part) })
	if err == nil {
		return nil
	}
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		err = pe.Err
	}
	return &fs.PathError{Op: opRemove, Path: at, Err: heldPast(err)}
}

// removeTree removes part in dir, and first, when it is a folder and not a link, everything in it.
func removeTree(dir *os.Root, part, at string, reached func(step string), owner int) error {
	look, err := dir.Lstat(part)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if look.IsDir() && linkKind(look.Mode()) == "" {
		reached(stepLooked)
		if err := emptied(dir, part, look, at, reached, owner); err != nil {
			return err
		}
	}
	return removeName(dir, part, at)
}

// emptied removes everything in the folder part in dir, given its owner's write bit first as
// ownerBits gives it, opened by next, which refuses it unless it is the folder look found, and closes it
// before the folder itself is removed.
func emptied(dir *os.Root, part string, look fs.FileInfo, at string, reached func(step string), owner int) (err error) {
	if err := ownerBits(dir, part, look, at, owner); err != nil {
		return err
	}
	sub, err := next(dir, part, look, at)
	if err != nil {
		return err
	}
	defer closing(sub, &err)
	f, err := sub.Open(".")
	if err != nil {
		return err
	}
	names, err := f.Readdirnames(-1)
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	for _, n := range names {
		if err := removeTree(sub, n, at+"/"+n, reached, owner); err != nil {
			return err
		}
	}
	return nil
}

// ownerBits gives the folder part in dir, which look found, its owner's write bit when owner owns
// it and it lacks the bit, keeping every other bit: by fchmod on the folder opened from dir and held
// to be the one look found, or refused as next refuses one, before next opens it as a root. Owner
// noOwner, a folder of another owner's, one without its owner's search bit, which next refuses,
// and one whose stat holds no owner, as Windows' does, are left as they are, and nothing is opened.
func ownerBits(dir *os.Root, part string, look fs.FileInfo, at string, owner int) error {
	if uid, ok := uidOf(look); !ok || uid != owner || look.Mode()&0o300 != 0o100 {
		return nil
	}
	f, err := dir.OpenFile(part, os.O_RDONLY|noWait|folderOnly, 0)
	if swappedAt(dir, err) {
		return changedAt(at)
	}
	if err != nil {
		return err
	}
	held, err := f.Stat()
	if err == nil && !os.SameFile(look, held) {
		err = changedAt(at)
	}
	if err == nil {
		if uid, ok := uidOf(held); ok && uid == owner && held.Mode()&0o100 != 0 {
			err = f.Chmod(held.Mode()&(fs.ModePerm|fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) | 0o200)
		}
	}
	return errors.Join(err, f.Close())
}

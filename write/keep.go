package write

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/birdtable/go-files/confine"
)

// tries is how often Append looks again after another writer moved the log.
const tries = 1000

// Keep makes a copy of file at name in folder, before the file is replaced, and answers the
// copy's path. name is the caller's, one plain name in folder on every system, as
// confine.Portable judges it: "", "." and "..", a separator, a colon, which names a stream of
// another file on Windows, an end in a dot or a space, which Windows strips, a Windows device as
// NUL, an 8.3 short name and .git are refused, on every system, before anything is opened. A taken name, a link among them, is
// refused with an error matching fs.ErrExist, never written over or followed, and nothing is
// written: the caller names another. The copy holds file's bytes, its mode and Owner's owner,
// flushed unless NoSync. The mode's group bits name file's group, so they stay only where the copy
// is in file's group, or in the group Owner gave it, and are cleared where it is not: a file 0640
// copied into a folder whose group the copy takes is 0600 there. Nothing is chowned to make the
// groups match; Owner alone gives the copy a group. Others' bits are kept as they are. file must be a plain file. A file that is missing makes no copy, and a
// copy that fails halfway is removed. Mode is not applied, and is refused where File refuses it;
// Shared is taken and not applied either, and with Mode is refused as File refuses it. On Windows,
// where file has no mode, the copy has the owner-only access list Option says, Shared given or not,
// and file's read-only attribute, and a volume that keeps no access lists is refused with
// ErrNoAccessList.
func Keep(file, folder, name string, opts ...Option) (string, error) {
	o, err := keeping(name, opts)
	if err != nil {
		return "", fmt.Errorf("a copy of %s in %s: %w", file, folder, err)
	}
	src, info, err := source(file, o)
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(folder)
	if err != nil {
		return "", errors.Join(err, src.Close())
	}
	err = errors.Join(errors.Join(copyTo(root, name, src, info, o), root.Close()), src.Close())
	if err != nil {
		return "", fmt.Errorf("a copy of %s in %s: %w", file, folder, err)
	}
	return filepath.Join(folder, name), nil
}

// KeepIn makes a copy of file, a name beneath from, at kept, a name beneath to, as Keep makes one,
// with Keep's refusals, kept's last element judged as Keep judges name, and Keep's group: the
// mode's group bits stay only where the copy is in file's group, or Owner's. from and to may be
// one root, or two that share no folder and lie on two file systems: no step crosses them by a
// rename or a link. kept's folder must be there: none is made. Every step goes through the root
// its name is beneath, as FileIn's do, so a name, or a link or folder swapped in on the way, that
// leads out of its root is refused, and nothing outside the two roots is read or written. file is
// opened through from; kept's folder is opened once through to, and the copy is made, and a
// half-made one removed, by its last name under that folder, as Keep's is under its folder.
func KeepIn(from *os.Root, file string, to *os.Root, kept string, opts ...Option) error {
	o, err := keeping(filepath.Base(kept), opts)
	if err == nil {
		err = errors.Join(named(from, file), named(to, kept))
	}
	var src *os.File
	var info fs.FileInfo
	if err == nil {
		src, info, err = regular(from, file, o)
	}
	if err == nil {
		err = errors.Join(copyIn(to, kept, src, info, o), src.Close())
	}
	if err != nil {
		return fmt.Errorf("a copy of %s beneath %s at %s beneath %s: %w", file, from.Name(), kept, to.Name(), err)
	}
	return nil
}

// copyIn opens kept's folder once through root and makes the copy by kept's last name under it.
func copyIn(root *os.Root, kept string, src *os.File, info fs.FileInfo, o options) error {
	folder, err := root.OpenRoot(filepath.Dir(kept))
	if err != nil {
		return err
	}
	return errors.Join(copyTo(folder, filepath.Base(kept), src, info, o), folder.Close())
}

// keeping refuses a copy's name that is not plain, Shared with Mode, and Gone, before anything is
// opened, and answers a copy's options: Shared, as Mode, is taken and not applied.
func keeping(name string, opts []Option) (options, error) {
	if err := plainName(name); err != nil {
		return options{}, err
	}
	o := settle(opts)
	if o.mixed {
		return o, errMixed
	}
	if o.gone {
		return o, errGone
	}
	o.copied, o.shared = true, false
	return o, nil
}

// copyTo makes name beneath root by an exclusive create and fills it from src, with info's mode. A
// copy that fails halfway is removed, and unless NoSync its folder is flushed.
func copyTo(root *os.Root, name string, src *os.File, info fs.FileInfo, o options) error {
	f, err := newFile(root, name, os.O_WRONLY, o)
	if err != nil {
		return err
	}
	o.from, o.given = info, o.gid
	if err := errors.Join(fill(f, src, info.Mode().Perm(), o), f.Close()); err != nil {
		return errors.Join(err, root.Remove(name))
	}
	if o.noSync {
		return nil
	}
	if err := syncFolder(root, filepath.Dir(name)); err != nil {
		return fmt.Errorf("%s %w: %w", name, ErrNotDurable, err)
	}
	return nil
}

// plainName refuses a copy's name that is not one plain name in its folder on every system, as
// confine.Portable judges it, so a name a program picks on one system is not another file's on
// the next: "v12." is v12 on Windows, and the path Keep answers would name another file.
func plainName(name string) error {
	if _, err := confine.Portable(name); err != nil {
		return fmt.Errorf("%q is not one plain name in the folder, so no copy is made: %w", name, err)
	}
	return nil
}

func source(file string, o options) (*os.File, fs.FileInfo, error) {
	dir, err := os.OpenRoot(filepath.Dir(file))
	if err != nil {
		return nil, nil, err
	}
	f, fi, err := regular(dir, filepath.Base(file), o)
	if err := errors.Join(err, dir.Close()); err != nil {
		if f != nil {
			err = errors.Join(err, f.Close())
		}
		return nil, nil, fmt.Errorf("%s: %w", file, err)
	}
	return f, fi, nil
}

// PutBack puts kept, a copy Keep made, back in file's place, with kept's bytes and mode (on Windows its
// read-only attribute, with the owner-only access list, whether or not opts hold a Mode or Shared), replaced
// whole as File replaces it. holds is the SHA-256 of what the caller expects file to hold now: a
// file that holds anything else, or is not there, is refused with ErrChanged, so a restore never
// goes over a change; Gone is refused, as it is PutBackIn's alone. kept's mode is Keep's: its group bits name kept's group, and stay only where
// the file put back is in kept's group, or Owner's, as Keep's copy's do. file's folder is opened once, and the file is hashed and replaced through
// it. A link or a pipe at file or at kept is refused, and not as ErrChanged. opts are File's, and
// the mode is kept's: a Mode in opts is not applied, and is refused where File refuses it, and a
// Shared is taken and not applied, and with Mode is refused as File refuses it. On
// Windows its rename is tried again for about 600 ms, as File's is.
func PutBack(kept, file string, holds [sha256.Size]byte, opts ...Option) error {
	if settle(opts).gone {
		return fmt.Errorf("%s: %w", file, errGone)
	}
	src, info, err := source(kept, settle(opts))
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(file))
	if err != nil {
		return errors.Join(fmt.Errorf("%s %w: %w", file, ErrChanged, err), src.Close())
	}
	err = putFrom(root, filepath.Base(file), src, info, holds, opts)
	if err := errors.Join(errors.Join(err, root.Close()), src.Close()); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	return nil
}

// PutBackIn puts kept, a name beneath from, back in the place of file, a name beneath to, as
// PutBack puts a copy back, with PutBack's refusals, options and group: a file that holds anything
// but holds, or is not there, is refused with ErrChanged, and with Gone it goes only where no file
// is, as Gone says. from and to may be one root or two, as KeepIn's may: kept is read through
// from, and file is hashed and replaced through to, by a temp file beside it, so no rename or link
// crosses the roots. Every step goes through the root its name is beneath, as KeepIn's do. On
// Windows its rename is tried again for about 600 ms, as File's is.
func PutBackIn(from *os.Root, kept string, to *os.Root, file string, holds [sha256.Size]byte, opts ...Option) error {
	err := named(from, kept)
	var src *os.File
	var info fs.FileInfo
	if err == nil {
		src, info, err = regular(from, kept, settle(opts))
	}
	if err != nil {
		return fmt.Errorf("%s beneath %s: %w", kept, from.Name(), err)
	}
	if err := errors.Join(putFrom(to, file, src, info, holds, opts), src.Close()); err != nil {
		return fmt.Errorf("%s beneath %s: %w", file, to.Name(), err)
	}
	return nil
}

// putFrom puts src, a copy with info's mode, at file beneath root, only over a file that holds holds,
// or under Gone by NoReplace's link, holds zero, and anything at the name ErrChanged.
func putFrom(root *os.Root, file string, src io.Reader, info fs.FileInfo, holds [sha256.Size]byte, opts []Option) error {
	o := settle(append(append([]Option(nil), opts...), IfHolds(holds)))
	gone := o.gone
	if gone {
		if holds != ([sha256.Size]byte{}) {
			return errGoneHolds
		}
		o.gone, o.holds, o.noReplace = false, nil, true
	}
	o.perm = info.Mode().Perm()      // kept's, set here and not by Mode or Shared
	o.from, o.given = info, o.gid    // whose group, or Owner's, the group bits name (grouped)
	o.copied, o.shared = true, false // put still refuses Shared with Mode (mixed)
	err := put(root, file, src, o)
	if gone && errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%w, as something is at the name, and Gone puts the copy back only where nothing is: %w", ErrChanged, err)
	}
	return err
}

// errGoneHolds refuses Gone with a holds that is not zero: the caller has not said which it means.
var errGoneHolds = fmt.Errorf("Gone puts a copy back where no file is, and holds is not zero, which names a file that is: %w", fs.ErrInvalid)

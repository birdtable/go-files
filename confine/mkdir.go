package confine

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// The steps of Mkdir where another process could change a folder, for a test's reached.
const (
	stepMadeFolder   = "between the make of a folder and its open"
	stepOpenedFolder = "between the open of a folder and its open as a root"
	stepFlushed      = "the folder above a made one flushed"
)

// ownerReadSearch is the bits of a mode Mkdir needs: this account reads and searches a folder made.
const ownerReadSearch fs.FileMode = 0o500

// Mkdir makes each missing folder of name beneath root, and answers the last as a root of its
// own, with no link followed at any component. Each folder is made in the handle of the folder
// above it, then opened with no link followed and checked: a link, or anything that is not a
// folder, at any component is refused with an error matching ErrOutside, and nothing is made
// through it or done to it. A folder this call made must still be one when opened, this
// account's and empty, and on Windows hold the owner-only list it was made with, read from its
// handle: one swapped in for it before its mode is set, a link, another account's folder, a
// folder that holds anything, renamed from beside it, or on Windows one with any other list, is
// refused with ErrOutside, its mode not changed and nothing removed. A folder made that holds
// anything is refused with ErrNotEmpty, which matches ErrOutside, whether it was swapped in or
// another process put a name in the folder made, as one that found it there does. One swap is not refused:
// an empty folder of this account, with that list on Windows, renamed
// in at the name, by an account that may write the folder above, is taken as the one made and
// given perm, as mkdirat answers with no handle and nothing tells that folder from a new one.
// A folder above made sticky, as a shared one is, refuses another account
// that rename. The answered root is held to be the very folder opened, so no swap on the way can
// redirect what the caller makes in it. A folder that was there keeps its mode. name is clean
// and local, as Local gives it; "." answers root itself, opened again, and makes nothing.
//
// perm must give this account read and search (0o500), so the folder can be opened to set its
// mode and walked into; one that does not, or has bits beyond the nine permission bits, is
// refused with fs.ErrInvalid before anything is made.
//
// On Unix perm, the nine permission bits alone, is set exactly on the handle of each folder made,
// whatever the umask, and a setgid bit the folder takes from the one above it is kept; the folder
// above each one made is flushed, so the new folder survives a power cut. On Windows, where a
// folder has an access list and no mode, each folder is made with an access list that only this
// account and SYSTEM reach, none of the folder above's inherited, which what is made in it
// inherits: narrower than perm's group and other bits, which are taken. A perm without owner read,
// write and search (0o700), or with any bit beyond the nine permission bits, is refused with
// ErrNoMode before anything is made, as the list would give more than it asks (one refused with
// fs.ErrInvalid above matches both), and a volume that
// keeps no access lists (FAT, exFAT) with ErrNoAccessList, at the first folder to make. Nothing is
// flushed there, as Windows cannot flush a folder. So
// Mkdir(root, name, 0o755) there still makes the owner-only list: a folder other accounts reach is
// made with MkdirShared.
//
// A refusal is an *fs.PathError naming, slash-separated, the component it stopped at.
func Mkdir(root *os.Root, name string, perm fs.FileMode) (*os.Root, error) {
	if err := clean(name); err != nil {
		return nil, err
	}
	if err := heldPerm(perm); err != nil { // first, so Windows answers every perm it refuses with ErrNoMode
		return nil, &fs.PathError{Op: "mkdir", Path: filepath.ToSlash(name), Err: err}
	}
	if perm&^fs.ModePerm != 0 {
		return nil, &fs.PathError{Op: "mkdir", Path: filepath.ToSlash(name), Err: fmt.Errorf("the mode %v has bits beyond the nine permission bits: %w", perm, fs.ErrInvalid)}
	}
	if perm&ownerReadSearch != ownerReadSearch {
		return nil, &fs.PathError{Op: "mkdir", Path: filepath.ToSlash(name), Err: fmt.Errorf("the mode %v does not give this account read and search, so a folder made could not be opened to set it or walked into: %w", perm, fs.ErrInvalid)}
	}
	return mkdir(root, name, perm, func(string) {}, false)
}

// MkdirPrivate makes each missing folder of name beneath root as only this account reaches it, and
// answers the last as a root of its own: Mkdir(root, name, 0o700) in every respect, on every system.
func MkdirPrivate(root *os.Root, name string) (*os.Root, error) { return Mkdir(root, name, 0o700) }

// MkdirShared makes each missing folder of name beneath root as other accounts reach it, and answers
// the last as a root of its own. On Unix it is Mkdir(root, name, 0o755) in every respect.
//
// On Windows, where 0o755 has no meaning, each missing folder is made with no access list of its
// own, so it inherits the inheritable entries of the folder above, as a folder any program makes
// there does: who reaches it is then that folder's say, which can be wider or narrower than 0o755. A
// folder it made must still be a folder when opened, this account's, or Administrators' where this
// token is elevated, and empty; it is not held to the owner-only list, as it was not made with one,
// but every entry of its list must be inherited. A folder swapped in for one made with an entry of
// its own or a protected list is refused with ErrOutside, and so is every folder made under a
// folder above with no inheritable entry, which Windows gives the token's default list. The check
// reads that an entry was inherited, not from which folder.
// Every other refusal of Mkdir stands: a link, a junction or anything that is not a folder at any
// component, and a folder of another account or one that holds anything swapped in for one made,
// are refused with ErrOutside, and a volume that keeps no access lists (FAT, exFAT), whose folder
// has no owner to check, with ErrNoAccessList at the first folder to make.
func MkdirShared(root *os.Root, name string) (*os.Root, error) {
	if err := clean(name); err != nil {
		return nil, err
	}
	return mkdirAs(root, name, 0o755, inherits, func(string) {}, false)
}

// kind is how a folder Mkdir makes is listed on Windows; Unix reads its perm alone. A test's third
// kind, Windows' alone, is inheritsElsewhere.
type kind uint8

const (
	ownerOnly kind = iota // Mkdir's: the owner-only list, and held to it once opened (listedAs)
	inherits              // MkdirShared's: no list of its own, and held to its owner alone (listedAs)
)

// walk is what each step of one Mkdir makes its folders by: their perm, their kind, the test's
// reached, called at each step, and listless, set by a test alone and read only under
// testing.Testing() (in winapi), which takes every volume as one that keeps no access lists.
type walk struct {
	perm     fs.FileMode
	kind     kind
	reached  func(step string)
	listless bool
}

// mkdir is Mkdir, calling reached at each of its steps, so a test of this package swaps there.
func mkdir(root *os.Root, name string, perm fs.FileMode, reached func(step string), listless bool) (*os.Root, error) {
	return mkdirAs(root, name, perm, ownerOnly, reached, listless)
}

// mkdirAs is mkdir, each folder made listed as k says.
func mkdirAs(root *os.Root, name string, perm fs.FileMode, k kind, reached func(step string), listless bool) (*os.Root, error) {
	if err := rootOnPipes(root, name); err != nil && root == nil {
		return nil, err
	}
	if name == "." {
		return root.OpenRoot(".")
	}
	w := walk{perm: perm, kind: k, reached: reached, listless: listless}
	cur, at := root, ""
	for part := range strings.SplitSeq(name, string(filepath.Separator)) {
		at = filepath.ToSlash(filepath.Join(at, part))
		next, err := step(cur, part, at, w)
		if cur != root {
			err = errors.Join(err, cur.Close())
		}
		if err != nil {
			if next != nil {
				err = errors.Join(err, next.Close())
			}
			return nil, err
		}
		cur = next
	}
	return cur, nil
}

// step makes part in cur when it is missing, and answers it, checked, as a root.
func step(cur *os.Root, part, at string, w walk) (*os.Root, error) {
	mkErr := newFolder(cur, part, w.perm, w.kind, w.listless)
	made := mkErr == nil
	if !made && !errors.Is(mkErr, fs.ErrExist) {
		return nil, mkErr
	}
	if made {
		w.reached(stepMadeFolder)
	}
	next, err := into(cur, part, at, made, w)
	if err != nil || !made {
		return next, err
	}
	if err := flushRoot(cur); err != nil {
		return next, err
	}
	w.reached(stepFlushed)
	return next, nil
}

// into opens the folder part of cur with no link followed and never waiting, refuses what is not
// a folder, and, when this call made it, holds it to the list w's kind made it with (listedAs) and
// to be fresh, and sets w's perm on that handle. It answers the folder as a root of its own, held
// to be the very folder opened.
func into(cur *os.Root, part, at string, made bool, w walk) (_ *os.Root, err error) {
	f, err := Open(cur, part, os.O_RDONLY|folderOnly, 0)
	if err != nil {
		return nil, atComponent(err, at)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	held, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !held.IsDir() {
		return nil, notFolder(at)
	}
	if made {
		if err := listedAs(f, at, w.kind); err != nil {
			return nil, err
		}
		if err := fresh(f, held, at, os.Geteuid()); err != nil {
			return nil, err
		}
		if err := folderMode(f, held, w.perm); err != nil {
			return nil, err
		}
	}
	w.reached(stepOpenedFolder)
	next, err := cur.OpenRoot(part)
	if err != nil {
		return nil, err
	}
	got, err := next.Stat(".")
	if err == nil && !os.SameFile(got, held) {
		err = &fs.PathError{Op: opWalk, Path: at, Err: fmt.Errorf("the folder changed between its open and its open as a root, and nothing was done in it: %w", ErrOutside)}
	}
	if err == nil {
		err = indexRoot(next, got, at, &fs.PathError{Op: opWalk, Path: at, Err: fmt.Errorf("the folder changed between its open and the read of its file index, and nothing was done in it: %w", ErrOutside)})
	}
	if err != nil {
		return nil, errors.Join(err, next.Close())
	}
	return next, nil
}

// fresh holds f, the folder at at opened right after this call made one there, to what a folder
// just made is: the account me's own, where the system gives a folder an owner, and empty. One
// renamed in at the name that is another account's is refused with ErrOutside, and one that holds
// anything, swapped in or filled by another process, with ErrNotEmpty, which matches it;
// an empty one of me's passes, as nothing tells it from the folder made (Mkdir's doc says so).
// The name found in it is not said: whoever swapped the folder chose it. On Windows, where a folder
// has a SID and no uid, its owner is held by listedAs first.
func fresh(f *os.File, held fs.FileInfo, at string, me int) error {
	if uid, ok := uidOf(held); ok && uid != me {
		return &fs.PathError{Op: opWalk, Path: at, Err: fmt.Errorf("the folder this call made is account %d's now, and this is account %d: it was swapped, and nothing was done to it: %w", uid, me, ErrOutside)}
	}
	_, err := f.ReadDir(1)
	switch {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil:
		return err
	}
	return &fs.PathError{Op: opWalk, Path: at, Err: fmt.Errorf("%w: another process put a name in it or swapped it, and nothing was done to it", ErrNotEmpty)}
}

func notFolder(at string) error {
	return &fs.PathError{Op: opWalk, Path: at, Err: fmt.Errorf("it is not a folder: %w", ErrOutside)}
}

// atComponent is err, Open's of a single component, naming the component by its path from the root
// Mkdir was handed; a component that is not a folder, refused at the open, is refused as one.
func atComponent(err error, at string) error {
	if openedNotFolder(err) {
		return notFolder(at)
	}
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return &fs.PathError{Op: pe.Op, Path: at, Err: pe.Err}
	}
	return err
}

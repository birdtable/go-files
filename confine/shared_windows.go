//go:build windows

package confine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"golang.org/x/sys/windows"

	"github.com/birdtable/go-files/internal/winapi"
)

// linkCount is the count of names the volume reports for the file f is a handle to, its
// NumberOfLinks, read from the handle: Windows' stat does not carry it.
func linkCount(f *os.File, _ fs.FileInfo) (int, error) {
	var d windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &d); err != nil {
		return 0, err
	}
	return countOf(uint64(d.NumberOfLinks)), nil
}

// noWait is nothing: an open of a name never waits for a pipe's other end here.
const noWait = 0

// folderOnly is nothing: the check of the opened handle refuses anything that is not a folder.
const folderOnly = 0

func openedNotFolder(error) bool { return false }

// uidOf is nothing: a folder here has a SID, and only the empty half of fresh holds it.
func uidOf(fs.FileInfo) (int, bool) { return -1, false }

// folderMode is nothing: a new folder has the owner-only list it was made with (newFolder).
func folderMode(*os.File, fs.FileInfo, fs.FileMode) error { return nil }

// inheritsElsewhere is inherits with this account taken as another in the owner check, a test's
// alone and read only under testing.Testing() (winapi.Inherited), as no plain token can make a folder
// another account owns.
const inheritsElsewhere = inherits + 1

// listedAs holds f, the folder at at that this call made, to the list newFolder made it with, read
// from f's own handle. Mkdir's (ownerOnly) is this account's, protected, granting SYSTEM and this
// account alone (winapi.Private): one swapped in at the name since, with any other owner or list,
// is refused with ErrOutside, and nothing is done to it or removed.
// MkdirShared's (inherits) has no list of its own, so it is held to that: this account's, or
// Administrators' where this token is elevated, with a list that is not protected and whose every
// entry is inherited (winapi.Inherited). One swapped in with any other owner, an entry of its own
// or a protected list is refused with ErrOutside the same way.
func listedAs(f *os.File, at string, k kind) error {
	if k == ownerOnly {
		ok, err := winapi.Private(windows.Handle(f.Fd()))
		if err == nil && !ok {
			err = &fs.PathError{Op: opWalk, Path: at, Err: fmt.Errorf("the folder this call made has another owner or access list now: it was swapped, and nothing was done to it: %w", ErrOutside)}
		}
		return err
	}
	ok, err := winapi.Inherited(windows.Handle(f.Fd()), k == inheritsElsewhere)
	if err == nil && !ok {
		err = &fs.PathError{Op: opWalk, Path: at, Err: fmt.Errorf("the folder this call made has another owner or an access list of its own now: it was swapped, and nothing was done to it: %w", ErrOutside)}
	}
	return err
}

// heldPerm refuses, with ErrNoMode, a perm the owner-only access list would give more than: one
// without owner read, write and search (0o700), or with any bit beyond the nine permission bits,
// as setuid, setgid and sticky are. Group and other bits are taken, and the list is narrower than
// they ask. A perm Mkdir refuses on every system with fs.ErrInvalid, bits beyond the nine or no
// owner read and search, matches it too, as it does on Unix.
func heldPerm(perm fs.FileMode) error {
	switch {
	case perm&^fs.ModePerm != 0 || perm&ownerReadSearch != ownerReadSearch:
		return fmt.Errorf("the mode %v: %w: %w", perm, ErrNoMode, fs.ErrInvalid)
	case perm&0o700 != 0o700:
		return fmt.Errorf("the mode %v: %w", perm, ErrNoMode)
	}
	return nil
}

// newFolder makes part in cur with the owner-only access list (winapi.MakeFolder), which what is made
// in it inherits, or, for MkdirShared, with no list of its own, inheriting the folder above's
// (winapi.MakeFolderShared). A volume that keeps no access lists is refused with ErrNoAccessList
// either way; listless takes every volume as one (mkdir).
func newFolder(cur *os.Root, part string, _ fs.FileMode, k kind, listless bool) error {
	refuse := winapi.Listless{Err: ErrNoAccessList, Test: listless}
	if k != ownerOnly {
		return winapi.MakeFolderShared(cur.Open, part, refuse)
	}
	return winapi.MakeFolder(cur.Open, part, refuse)
}

// flushRoot is nothing: Windows cannot open a folder to flush it.
func flushRoot(*os.Root) error { return nil }

// retried is op, tried again for about 600 ms while another process holds the file (winapi.Retried).
func retried(op func() error) error { return winapi.Retried(op) }

// heldPast is err, a removal refused past the window, made to match fs.ErrPermission where it is a
// sharing violation, as a denial already does.
func heldPast(err error) error {
	if errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("%w, and it was held open past the window: %w", err, fs.ErrPermission)
	}
	return err
}

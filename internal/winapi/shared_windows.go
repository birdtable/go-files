package winapi

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// MakeFileShared makes the file at path as MakeFile does, by an exclusive create in its folder,
// opened by open, that follows no reparse point, and answers the create's own handle, open for
// access and to stat, sharing as share says; but with no descriptor of its own, so its access list
// is the inheritable entries of the folder it is made in and nothing else, as a file any program
// makes there has, and who reaches it is that folder's say. A volume that keeps no access lists
// (FAT, exFAT) makes it with none, as there is no list to give. No probe is made: what this account
// cannot reach of it, the caller's next open by name finds.
func MakeFileShared(open Open, path string, access, share uint32) (*os.File, error) {
	return madeFile(path, func(took func(windows.Handle) error) error {
		return makeInherited(open, path, fileHow(access, share), nil, took)
	})
}

// MakeFolderShared makes the folder at path in its folder, opened by open, with no descriptor of its
// own, so it inherits the folder above's inheritable entries, and closes it. A volume that keeps no
// access lists, or will not say whether it does, is refused with refuse.Err, and nothing is made, as
// MakeFolder refuses it: a folder there has no owner the caller's check of what it made could read.
func MakeFolderShared(open Open, path string, refuse Listless) error {
	err := makeInherited(open, path, How{Access: windows.FILE_LIST_DIRECTORY, Share: windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE,
		Options: windows.FILE_DIRECTORY_FILE | windows.FILE_OPEN_FOR_BACKUP_INTENT, Attributes: windows.OBJ_CASE_INSENSITIVE}, &refuse,
		windows.CloseHandle)
	if err != nil {
		return &fs.PathError{Op: "mkdir", Path: path, Err: err}
	}
	return nil
}

// makeInherited makes path's last name in its folder, opened by open, by an exclusive create that
// follows no reparse point, as how says, with no descriptor, and hands the handle made to took. A
// last name ending in a dot or a space is ErrStrippedName, before anything is opened. With refuse,
// a volume that keeps no access lists is refused as makePrivate refuses it (keepsLists). A name
// there already, a link among them, is an error matching fs.ErrExist, a denied make answered as
// makePrivate answers it (there). An open error is a Win32 one (Dos).
func makeInherited(open Open, path string, how How, refuse *Listless, took func(windows.Handle) error) (err error) {
	if err := Stripped(path); err != nil {
		return err
	}
	d, err := open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	if refuse != nil {
		if err := keepsLists(d, path, *refuse); err != nil {
			return err
		}
	}
	how.Disposition = windows.FILE_CREATE
	h, err := OpenAt(windows.Handle(d.Fd()), filepath.Base(path), how)
	if err != nil {
		if err = Dos(err); errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return there(d, path, err)
		}
		return err
	}
	return took(h)
}

// keepsLists is nil when the volume of the folder d keeps access lists. A volume that keeps none, or
// will not say whether it does, is refuse.Err, or, when path's last name is there already, an error
// matching fs.ErrExist (there), so a caller that opens what is there does as it did. Under
// testing.Testing(), refuse's Test takes every volume as one that keeps none, and its fault
// volumeUnsaid as one that will not say.
func keepsLists(d *os.File, path string, refuse Listless) error {
	var flags uint32
	err := windows.GetVolumeInformationByHandle(windows.Handle(d.Fd()), nil, 0, nil, nil, &flags, nil, 0)
	if refuse.fault == volumeUnsaid && testing.Testing() {
		err = windows.ERROR_INVALID_FUNCTION
	}
	if err != nil {
		return there(d, path, fmt.Errorf("%w: the volume would not say whether it keeps access lists: %w", refuse.Err, err))
	}
	if flags&windows.FILE_PERSISTENT_ACLS == 0 || refuse.Test && testing.Testing() {
		return there(d, path, refuse.Err)
	}
	return nil
}

// Inherited is whether the folder open at h, opened with READ_CONTROL, is as MakeFolderShared makes
// one: owned as what this account makes with no descriptor is, by this account, or by
// Administrators when this token is elevated, and with a list that is not protected and whose every
// entry is inherited from the folder above, so who reaches it is that folder's say. A folder
// swapped in at the name since, with another owner, an entry of its own, a protected list, no list
// or a null one, is not; nor is one made where the folder above has no inheritable entry, which the
// system lists by the token's default, so the caller refuses it rather than answer it. elsewhereTest, set by a package's test alone and read only under
// testing.Testing(), takes this account as elsewhere and the token as not elevated, so a test
// reaches the answer for a folder another account owns, which a plain token cannot make.
func Inherited(h windows.Handle, elsewhereTest bool) (bool, error) {
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false, err
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return false, err
	}
	me, err := self()
	elevated := windows.GetCurrentThreadEffectiveToken().IsElevated()
	if elsewhereTest && testing.Testing() {
		me, err = windows.StringToSid(elsewhere)
		elevated = false
	}
	if err != nil || !owner.Equals(me) && (!elevated || !owner.IsWellKnown(windows.WinBuiltinAdministratorsSid)) {
		return false, err
	}
	control, _, err := sd.Control()
	if err != nil {
		return false, err
	}
	dacl, _, err := sd.DACL()
	switch {
	case errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND), err == nil && dacl == nil, err == nil && control&windows.SE_DACL_PROTECTED != 0:
		return false, nil
	case err != nil:
		return false, err
	}
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE // every kind of entry starts with the header read here
		if err := windows.GetAce(dacl, i, &ace); err != nil || ace.Header.AceFlags&windows.INHERITED_ACE == 0 {
			return false, err
		}
	}
	return true, nil
}

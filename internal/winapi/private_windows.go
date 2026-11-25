package winapi

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ErrStrippedName is a make's refusal of a last name that ends in a dot or a space: NtCreateFile
// makes that name as written, while os.Root, os.Lstat and every Win32 call after it read the name
// with the end stripped, so what is made is reached by no later call, and another name is. It
// matches fs.ErrInvalid, as write's and confine's own refusals of such a name do.
var ErrStrippedName = fmt.Errorf("the name ends in a dot or a space, which the Win32 name rules strip, so it would make one name and every later call would reach another: %w", fs.ErrInvalid)

// Stripped is ErrStrippedName when path's last name ends in a dot or a space, and nil otherwise:
// the check of every make here, and of write.Append's door, which looks by os.Root and would find
// the stripped name's log.
func Stripped(path string) error {
	if name := filepath.Base(path); strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return ErrStrippedName
	}
	return nil
}

// Open is how a maker opens the folder it makes in: os.Open, or an os.Root's Open beneath it.
type Open func(name string) (*os.File, error)

// Listless is a package's refusal of a volume where the owner-only list cannot be had: one that
// keeps no access lists, one that will not say whether it keeps them, and one that will not take
// this account as the owner. Err is the error it answers, its own ErrNoAccessList. Test, set by
// that package's test alone and read only under testing.Testing(), takes every volume as one that
// keeps none, which no test machine's NTFS is.
type Listless struct {
	Err  error
	Test bool
	// fault, this package's tests' alone and read only under testing.Testing(), makes the volume
	// answer as a share or a volume no test machine has.
	fault fault
}

// fault is a volume's answer a test of makePrivate asks for.
type fault uint8

const (
	// volumeUnsaid: GetVolumeInformationByHandle fails, as a volume that will not say answers.
	volumeUnsaid fault = iota + 1
	// ownerRefused: the make names an owner the token does not hold, which the system refuses as
	// a share that knows this account by another SID refuses the local one.
	ownerRefused
	// listElsewhere: the make's list grants elsewhere in place of this account, the owner left as
	// this account, as a share that takes the list as sent and knows this account by another SID
	// keeps it, so the make's handle is granted and an open by name is not.
	listElsewhere
	// readOnly: the list grants this account read alone, as a write-restricted token reads it.
	readOnly
)

// elsewhere is an account no test machine's token holds, the owner ownerRefused names, the
// account listElsewhere's list grants, and the account Mine takes as this one for a test.
const elsewhere = "S-1-5-21-1-2-3-1001"

// MakeFile makes the file at path by an exclusive create in its folder, opened by open, with the
// owner-only descriptor (makePrivate), and answers the create's own handle, open for access and to
// stat, sharing as share says. share is the caller's: a lock file shares no delete, so it cannot be
// removed while it is held. The handle has no DELETE, so another open of the name need not share
// delete, as a lock's contender's does not.
func MakeFile(open Open, path string, access, share uint32, refuse Listless) (*os.File, error) {
	return madeFile(path, func(took func(windows.Handle) error) error {
		return makePrivate(open, path, fileHow(access, share), refuse, took)
	})
}

// fileHow is how MakeFile and MakeFileShared open the file they make: for access and to stat,
// sharing as share says, and never a folder.
func fileHow(access, share uint32) How {
	return How{Access: access | windows.STANDARD_RIGHTS_READ | windows.FILE_READ_ATTRIBUTES | windows.FILE_READ_EA,
		Share: share, Options: windows.FILE_NON_DIRECTORY_FILE | windows.FILE_OPEN_FOR_BACKUP_INTENT,
		Attributes: windows.OBJ_CASE_INSENSITIVE}
}

// madeFile answers the file mk makes, its handle as an *os.File named path. A make that fails
// after its handle was taken, as when the folder's close fails, closes the file too, and its error
// is an *fs.PathError of the open.
func madeFile(path string, mk func(took func(windows.Handle) error) error) (*os.File, error) {
	var f *os.File
	err := mk(func(h windows.Handle) error {
		f = os.NewFile(uintptr(h), path)
		return nil
	})
	if err != nil {
		if f != nil { // made, and the folder's close failed
			err = errors.Join(err, f.Close())
		}
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	return f, nil
}

// MakeFolder makes the folder at path in its folder, opened by open, with the owner-only
// descriptor (makePrivate), and closes it.
func MakeFolder(open Open, path string, refuse Listless) error {
	err := makePrivate(open, path, How{Access: windows.FILE_LIST_DIRECTORY, Share: windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE,
		Options: windows.FILE_DIRECTORY_FILE | windows.FILE_OPEN_FOR_BACKUP_INTENT, Attributes: windows.OBJ_CASE_INSENSITIVE}, refuse,
		windows.CloseHandle)
	if err != nil {
		return &fs.PathError{Op: "mkdir", Path: path, Err: err}
	}
	return nil
}

// makePrivate makes path's last name in its folder, opened by open, by an exclusive create that
// follows no reparse point, as how says, with the owner-only descriptor (list), so the file or
// folder is never there with another list, and hands the handle made to took, to keep or close. A
// last name ending in a dot or a space is ErrStrippedName, before anything is opened. A name there
// already, a link among them, is an error matching fs.ErrExist: a file's make at a folder is
// answered access denied, as the folder is refused for FILE_NON_DIRECTORY_FILE before the
// create's collision is seen, so a denied make looks for the name and answers it as there when it
// is. A volume that keeps no access lists, as FAT and exFAT, would make
// it with none, so a missing name there is refused with refuse.Err, and nothing is made; one there
// already is still an error matching fs.ErrExist, so a caller that opens what is there does as it
// did. A volume that will not say whether it keeps lists is answered so too, and a make the volume
// refuses for its owner, as a share that knows this account by another SID refuses it, is
// refuse.Err with nothing made. An open error is a Win32 one (Dos).
//
// Before path's name is made, the descriptor is tried on a throwaway name in the same folder
// (probe): one this account cannot then open by name to read and write is refuse.Err, with the
// denial, and nothing is made at path: under a restricted token, write-restricted or not, or an
// AppContainer, whose checks also ask for SIDs the list does not name, or on a share that keeps
// the list as sent and knows this account by another SID, what is made with this account's list
// is still not its to reach. No SID is added: a restricted token's are mostly ones plain tokens
// hold. A name there already is answered as there before any probe is made (there), so a Take
// of a lock or a step of Mkdir over what is there makes nothing, as before the probe, also where
// nothing can be made, as on a read-only or full volume; any other answer of the look goes on, and
// a name made or removed since meets the exclusive create, as it did. The name at path is then
// made as the probe found it would be reached, with no DELETE, so no other open of it must share
// delete.
func makePrivate(open Open, path string, how How, refuse Listless, took func(windows.Handle) error) (err error) {
	if err := Stripped(path); err != nil {
		return err
	}
	d, err := open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	if err := keepsLists(d, path, refuse); err != nil {
		return err
	}
	if how.security, err = list(refuse, how.Options&windows.FILE_DIRECTORY_FILE != 0); err != nil {
		return err
	}
	if err := there(d, path, nil); errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return err
	}
	how.Disposition = windows.FILE_CREATE
	if err := probe(d, path, how, refuse.Err); err != nil {
		return err
	}
	h, err := OpenAt(windows.Handle(d.Fd()), filepath.Base(path), how)
	if err != nil {
		return refused(d, path, refuse.Err, err)
	}
	return took(h)
}

// probe makes a throwaway name in the folder d as how says, to DELETE too, opens it again by name
// (reached), and removes it through the make's handle: nil when this account reaches by name what
// it makes there with how's descriptor. The make's errors are answered as path's would be
// (refused); a name it cannot open is refuse, and nothing is made at path, or, when path is there
// already, an error matching fs.ErrExist, as on a volume that keeps no lists, so a caller that
// opens what is there does as it did. The probe's DELETE is on the throwaway name alone, which
// nobody else opens.
func probe(d *os.File, path string, how How, refuse error) error {
	name := ".probe-" + rand.Text()
	how.Access |= windows.DELETE
	h, err := OpenAt(windows.Handle(d.Fd()), name, how)
	if err != nil {
		return refused(d, path, refuse, err)
	}
	if err := reached(d, name); err != nil {
		kind := "file"
		if how.Options&windows.FILE_DIRECTORY_FILE != 0 {
			kind = "folder"
		}
		err = fmt.Errorf("%w: this account could not open by name a %s it made here with its own list, so nothing was made at the name: %w", refuse, kind, err)
		if rerr := removed(h); rerr != nil {
			return errors.Join(err, fmt.Errorf("the probe %s could not be removed: %w", filepath.Join(filepath.Dir(path), name), rerr))
		}
		return there(d, path, err) // a name there already is opened as it was
	}
	if err := removed(h); err != nil {
		return fmt.Errorf("nothing was made at the name: the probe %s could not be removed: %w", filepath.Join(filepath.Dir(path), name), err)
	}
	return nil
}

// refused is what a make's exclusive create, at path or at the probe's name beside it, answers
// for its error err: refuse when the volume will not take this account as the owner; a denial when
// path is not there (there), and ERROR_ALREADY_EXISTS when it is, as a file's make at a folder is
// denied and a folder this account may not write denies every make; err itself otherwise.
func refused(d *os.File, path string, refuse, err error) error {
	switch err = Dos(err); {
	case errors.Is(err, windows.ERROR_INVALID_OWNER):
		return fmt.Errorf("%w: the volume would not take this account as the owner: %w", refuse, err)
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return there(d, path, err)
	}
	return err
}

// list is the owner-only descriptor of a file or folder only this account reaches: its owner is
// the account the calling thread acts as, and its access list, protected so that nothing of the
// folder above is inherited, grants full access to that account and to SYSTEM alone; on a folder,
// what is made in it inherits the same two entries. Administrators are not granted: one reaches
// the file only by taking it, which the system records. It is the 0600 and 0700 of Unix. Under
// testing.Testing(), refuse's fault may ask for another.
func list(refuse Listless, folder bool) (*windows.SECURITY_DESCRIPTOR, error) {
	sid, err := self()
	if err != nil {
		return nil, err
	}
	owner, me, rights, inherit := sid.String(), sid.String(), "FA", ""
	switch {
	case !testing.Testing():
	case refuse.fault == ownerRefused:
		owner = elsewhere
	case refuse.fault == listElsewhere:
		me = elsewhere
	case refuse.fault == readOnly:
		rights = "FR"
	}
	if folder {
		inherit = "OICI"
	}
	return windows.SecurityDescriptorFromString("O:" + owner + "D:P(A;" + inherit + ";FA;;;SY)(A;" + inherit + ";" + rights + ";;;" + me + ")")
}

// self is the account the calling thread acts as: the owner list gives what is made, and the one
// account Mine and Narrow take as this one.
func self() (*windows.SID, error) {
	u, err := windows.GetCurrentThreadEffectiveToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return u.User.Sid, nil
}

// Mine is whether the file or folder open at h, opened with READ_CONTROL, is owned by this
// account, the owner the owner-only list gives what is made, so a file made with that list in its
// place has its owner exactly. A group this account is in is not this account: Administrators owns
// what an elevated program makes by default, and a file made with the list would not be theirs.
// elsewhereTest, set by a package's test alone and read only under testing.Testing(), takes this
// account as elsewhere, so a test reaches the answer for a file another account owns, which a
// plain token cannot make.
func Mine(h windows.Handle, elsewhereTest bool) (bool, error) {
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return false, err
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return false, err
	}
	me, err := self()
	if elsewhereTest && testing.Testing() {
		me, err = windows.StringToSid(elsewhere)
	}
	if err != nil {
		return false, err
	}
	return owner.Equals(me), nil
}

// Narrow is whether the access list of the file or folder open at h, opened with READ_CONTROL,
// grants nothing to any account but this one, SYSTEM and Administrators: every allowing entry
// names one of the three, and a denying entry grants nothing. What the owner-only list grants is
// then no less than what the old list granted anyone but Administrators, who reach it by taking it.
// No list, or a null one, grants every account everything, and is not narrow; nor is a list with
// an entry of any kind but the plain allow and the plain deny, an object or a callback entry among
// them, which is not read here, whoever it names.
//
//nolint:gosec // G103: an entry's SID starts at SidStart, as the system lays it out
func Narrow(h windows.Handle) (bool, error) {
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false, err
	}
	dacl, _, err := sd.DACL()
	switch {
	case errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND), err == nil && dacl == nil:
		return false, nil // no list, or a null one: every account reaches it
	case err != nil:
		return false, err
	}
	me, err := self()
	if err != nil {
		return false, err
	}
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return false, err
		}
		switch ace.Header.AceType {
		case windows.ACCESS_DENIED_ACE_TYPE:
			continue
		case windows.ACCESS_ALLOWED_ACE_TYPE:
			who := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
			if who.Equals(me) || who.IsWellKnown(windows.WinLocalSystemSid) || who.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
				continue
			}
		}
		return false, nil
	}
	return true, nil
}

// Private is whether the folder open at h, opened with READ_CONTROL, is as MakeFolder makes one:
// owned by this account, with a protected access list of exactly list's two entries, every right
// to SYSTEM and then to this account, each inherited by all the folder holds. A folder swapped in
// at the name since its make has another owner or another list, unless it was made so (second
// review on PR 262).
//
//nolint:gosec // G103: an entry's SID starts at SidStart, as the system lays it out
func Private(h windows.Handle) (bool, error) {
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false, err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return false, err
	}
	control, _, err := sd.Control()
	if err != nil {
		return false, err
	}
	dacl, _, err := sd.DACL()
	switch {
	case errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND), err == nil && dacl == nil:
		return false, nil // no list, or a null one
	case err != nil:
		return false, err
	}
	me, err := self()
	if err != nil || owner == nil || !owner.Equals(me) || control&windows.SE_DACL_PROTECTED == 0 || dacl.AceCount != 2 {
		return false, err
	}
	for i, want := range []func(*windows.SID) bool{func(s *windows.SID) bool { return s.IsWellKnown(windows.WinLocalSystemSid) }, me.Equals} {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(i), &ace); err != nil { //nolint:gosec // G115: i is 0 or 1
			return false, err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags != windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE ||
			ace.Mask != windows.STANDARD_RIGHTS_REQUIRED|windows.SYNCHRONIZE|0x1ff || !want((*windows.SID)(unsafe.Pointer(&ace.SidStart))) {
			return false, nil
		}
	}
	return true, nil
}

// reached opens name in the folder d again for FILE_READ_EA and FILE_WRITE_EA, and closes it,
// writing nothing: nil when this account reaches what it made by name. Neither the folder's list
// nor ownership grants either, and no backup intent is asked, so only the list's entry for this
// account does; a write-restricted token asks its restricted SIDs for writes alone, so a read
// would pass it. It shares all, so the make's handle does not stop it, and no share is checked
// for either right.
func reached(d *os.File, name string) error {
	h, err := OpenAt(windows.Handle(d.Fd()), name, How{Access: windows.FILE_READ_EA | windows.FILE_WRITE_EA,
		Share: windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE, Disposition: windows.FILE_OPEN,
		Attributes: windows.OBJ_CASE_INSENSITIVE})
	if err != nil {
		return Dos(err)
	}
	return windows.CloseHandle(h)
}

// removed removes what the make's handle h has open, and closes h: by its delete disposition with
// POSIX semantics, so the name leaves the folder at once though another process, as a scanner,
// holds it open, or, where the volume refuses that, by the classic disposition, as Rename falls
// back. The probe runs only on a volume that keeps access lists, so on NTFS or ReFS.
func removed(h windows.Handle) error {
	flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS)
	err := windows.SetFileInformationByHandle(h, windows.FileDispositionInfoEx, (*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags))) //nolint:gosec // G103: the pointer is to flags, a FILE_DISPOSITION_INFO_EX, for the one call
	if err != nil {
		del := byte(1) // FILE_DISPOSITION_INFO's DeleteFile
		err = windows.SetFileInformationByHandle(h, windows.FileDispositionInfo, &del, 1)
	}
	return errors.Join(err, windows.CloseHandle(h))
}

// there is ERROR_ALREADY_EXISTS when path's last name is in the folder d, opened with no reparse
// point followed, and refuse when nothing is: makePrivate's look before its probe, with refuse nil,
// its answer on a volume that keeps no access lists, where it makes nothing, and to a make denied.
func there(d *os.File, path string, refuse error) error {
	h, err := OpenAt(windows.Handle(d.Fd()), filepath.Base(path), How{Access: windows.FILE_READ_ATTRIBUTES,
		Share: windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE, Disposition: windows.FILE_OPEN,
		Options: windows.FILE_OPEN_FOR_BACKUP_INTENT, Attributes: windows.OBJ_CASE_INSENSITIVE})
	if err == nil {
		return errors.Join(windows.ERROR_ALREADY_EXISTS, windows.CloseHandle(h))
	}
	if err = Dos(err); errors.Is(err, fs.ErrNotExist) {
		return refuse
	}
	return err
}

//go:build windows

package trust

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"unsafe"

	"github.com/birdtable/go-files/internal/fileid"
	"golang.org/x/sys/windows"
)

// The owners Windows always trusts, rule 1 with this account: Administrators, SYSTEM and
// TrustedInstaller.
const (
	system           = "S-1-5-18"
	trustedInstaller = "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"
)

// reparseAFUnix is the reparse tag of a Unix socket's name, the one reparse point that is not a
// link (rule 5).
const reparseAFUnix = 0x80000023

// identify is SECURITY_SQOS_PRESENT with SECURITY_IDENTIFICATION, internal/winapi's Identify, which
// trust may not import: where an open lands on a named pipe, its server may learn who the caller
// is and act as it in nothing. The system reads the first as FILE_FLAG_OPEN_NO_RECALL too, which
// changes no read of trust's: a file in remote storage is a reparse point, which trust takes for a
// link (rule 5) and never reads.
const identify = windows.SECURITY_SQOS_PRESENT | windows.SECURITY_IDENTIFICATION

// createFile is windows.CreateFile with identify added to flags, trust's one CreateFile.
func createFile(name *uint16, access, share, disposition, flags uint32, template windows.Handle) (windows.Handle, error) {
	return windows.CreateFile(name, access, share, nil, disposition, flags|identify, template)
}

// local is this system's host: each name read from a handle opened on the name itself, the owners
// above and this account trusted, and broken when this account's own SID cannot be read.
func local() host {
	me, err := self()
	return host{look: look, ident: ident, readlink: os.Readlink, open: openRead, stat: statFile, //nolint:forbidigo // the walk reads a link in a folder it has judged, by an open of the link itself, never through it
		root: []string{administrators, system, trustedInstaller}, me: me, broken: err}
}

func self() (string, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("this account's SID, from its token: %w", err)
	}
	return sidText(u.User.Sid, "this account's SID")
}

// sidText is the SID s written as a string, or an error naming what as the SID that could not be
// written; x/sys writes such a SID as "".
func sidText(s *windows.SID, what string) (string, error) {
	t := s.String()
	if t == "" {
		return "", errors.New(what + " cannot be written as a string")
	}
	return t, nil
}

// look reads the facts of the one name path from a handle that may read its attributes and its
// security descriptor and nothing else, opened on a reparse point itself.
func look(path string) (node, error) {
	h, err := openName(path, windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES)
	if err != nil {
		return node{}, err
	}
	n, err := facts(h, path)
	if cerr := windows.CloseHandle(h); cerr != nil && err == nil {
		return node{}, &fs.PathError{Op: "close", Path: path, Err: cerr}
	}
	return n, err
}

// ident reads the file ID of what path names, following its links: the name is opened as
// CreateFile opens it by default, through a reparse point to its target, with the right to read
// its attributes alone.
func ident(path string) (id fileID, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fileID{}, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := createFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return fileID{}, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	f := os.NewFile(uintptr(h), path) // owns h, and closes it
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			id, err = fileID{}, &fs.PathError{Op: "close", Path: path, Err: cerr}
		}
	}()
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(h, &info); err != nil {
		return fileID{}, &fs.PathError{Op: "GetFileInformationByHandle", Path: path, Err: err}
	}
	// A file whose 64-bit index reads zero (a WebDAV or SMB drive) or all-ones (ReFS) has none
	// os.SameFile can tell it apart from another by, so the walk refuses it rather than trust the
	// compare; its ErrNoFileIndex is write.ErrNoFileIndex, so errors.Is holds with it.
	switch ok, ierr := fileid.Indexed(f, false); {
	case ierr != nil:
		return fileID{}, &fs.PathError{Op: "file index", Path: path, Err: ierr}
	case !ok:
		return fileID{}, &fs.PathError{Op: "file index", Path: path, Err: fileid.ErrNoFileIndex}
	}
	return idOf(&info), nil
}

// idOf is the file ID in info: its volume's serial number and its file index, as os.SameFile
// compares them.
func idOf(info *windows.ByHandleFileInformation) fileID {
	return fileID{vol: uint64(info.VolumeSerialNumber), idx: uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)}
}

// openName opens the name path with the rights access, sharing it with every other opener, on a
// reparse point itself (FILE_FLAG_OPEN_REPARSE_POINT), and on a folder as on a file
// (FILE_FLAG_BACKUP_SEMANTICS, which takes no privilege without one asked for).
func openName(path string, access uint32) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := createFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	return h, nil
}

// facts reads what the rules judge from the handle h, opened at path: its file ID, type, size,
// owner and access list. A descriptor that cannot be read is an error, so the name is never judged
// (rule 6).
func facts(h windows.Handle, path string) (node, error) {
	n, err := typeOf(h, path)
	if err != nil {
		return node{}, err
	}
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return node{}, &fs.PathError{Op: "GetSecurityInfo", Path: path, Err: err}
	}
	owner, _, err := sd.Owner()
	if err == nil && owner == nil {
		err = errors.New("the security descriptor names no owner")
	}
	if err != nil {
		return node{}, &fs.PathError{Op: "GetSecurityDescriptorOwner", Path: path, Err: err}
	}
	o, err := sidText(owner, "the owner's SID")
	if err != nil {
		return node{}, &fs.PathError{Op: "ConvertSidToStringSid", Path: path, Err: err}
	}
	d, err := entries(sd)
	if err != nil {
		return node{}, &fs.PathError{Op: "GetAce", Path: path, Err: err}
	}
	n.owner, n.dacl = o, d
	return n, nil
}

// typeOf is the file ID, type and size of the name open at h. A reparse point is a link (rule 5),
// but for a Unix socket's; a pipe, a console or a device is none of the kinds, and has no ID.
func typeOf(h windows.Handle, path string) (node, error) {
	ft, err := windows.GetFileType(h)
	if err != nil {
		return node{}, &fs.PathError{Op: "GetFileType", Path: path, Err: err}
	}
	if ft != windows.FILE_TYPE_DISK {
		return node{typ: fs.ModeDevice}, nil
	}
	var tag struct{ attributes, reparseTag uint32 }
	err = windows.GetFileInformationByHandleEx(h, windows.FileAttributeTagInfo, (*byte)(unsafe.Pointer(&tag)), uint32(unsafe.Sizeof(tag))) //nolint:gosec // G103: FILE_ATTRIBUTE_TAG_INFO is filled by address, and x/sys has no type for it
	if err != nil {
		return node{}, &fs.PathError{Op: "GetFileInformationByHandleEx", Path: path, Err: err}
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return node{}, &fs.PathError{Op: "GetFileInformationByHandle", Path: path, Err: err}
	}
	n := node{id: idOf(&info), size: int64(info.FileSizeHigh)<<32 | int64(info.FileSizeLow)}
	switch {
	case tag.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 && tag.reparseTag == reparseAFUnix:
		n.typ = fs.ModeSocket
	case tag.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0:
		n.typ = fs.ModeSymlink
	case tag.attributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0:
		n.typ = fs.ModeDir
	}
	return n, nil
}

// entries is the access list of sd. One that is absent, or present and null, lets anyone do
// anything, and is null here (rule 4). An entry of a type other than allow or deny keeps its type
// and mask, and no SID, since its layout differs; the rules refuse it.
func entries(sd *windows.SECURITY_DESCRIPTOR) (*dacl, error) {
	acl, _, err := sd.DACL()
	switch {
	case errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND):
		return &dacl{null: true}, nil
	case err != nil:
		return nil, err
	case acl == nil:
		return &dacl{null: true}, nil
	}
	// The list's size is its header's bytes 2 and 3, low byte first; x/sys keeps that field to itself.
	aclSize := uintptr(binary.LittleEndian.Uint16(unsafe.Slice((*byte)(unsafe.Pointer(acl)), 8)[2:])) //nolint:gosec // G103: the ACL header, 8 bytes, as winnt.h lays it out
	d := &dacl{aces: make([]ace, 0, acl.AceCount)}
	for i := range uint32(acl.AceCount) {
		var a *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &a); err != nil {
			return nil, err
		}
		e := ace{typ: a.Header.AceType, flags: a.Header.AceFlags, mask: uint32(a.Mask)}
		if e.typ == aceAllow || e.typ == aceDeny {
			// The entry is AceSize bytes from a, within the list's AclSize: its header and mask,
			// 8 bytes, then the SID.
			off := uintptr(unsafe.Pointer(a)) - uintptr(unsafe.Pointer(acl)) //nolint:gosec // G103: the entry's offset in the list, to bound its bytes
			size := uintptr(a.Header.AceSize)
			if size < 8 || off+size > aclSize {
				return nil, fmt.Errorf("access entry %d: %d bytes at %d, not within the list's %d", i, size, off, aclSize)
			}
			auth, subs, ok := sidFields(unsafe.Slice((*byte)(unsafe.Pointer(a)), size)[8:]) //nolint:gosec // G103: the entry's own bytes, bounded above
			if !ok {
				return nil, fmt.Errorf("access entry %d: no SID whole in its %d bytes", i, size)
			}
			e.sid, e.sandbox = sidString(auth, subs), appContainer(auth, subs)
		}
		d.aces = append(d.aces, e)
	}
	return d, nil
}

// openRead opens path to be read, on a reparse point itself, and refuses a name that is not a
// regular file before the handle is handed on, as O_NOFOLLOW does on Unix.
func openRead(path string) (*os.File, error) {
	h, err := openName(path, windows.GENERIC_READ)
	if err != nil {
		return nil, err
	}
	n, err := typeOf(h, path)
	if err == nil && n.typ != 0 {
		err = refuse(path, path+" is "+typeName(n.typ)+", not a regular file", Fix{}, ErrNotPlain)
	}
	if err != nil {
		if cerr := windows.CloseHandle(h); cerr != nil {
			return nil, errors.Join(err, &fs.PathError{Op: "close", Path: path, Err: cerr})
		}
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}

func statFile(f *os.File) (node, error) {
	return facts(windows.Handle(f.Fd()), f.Name())
}

// ownerOf is f's owner, from the security descriptor of its handle.
func ownerOf(f *os.File) (string, error) {
	n, err := facts(windows.Handle(f.Fd()), f.Name())
	return n.owner, err
}

// bySpelling is true: the system cleans a path by its spelling before it opens it, and takes a .. in
// a relative link's target against the link's own folder, so the walk cleans them so too.
const bySpelling = true

// nameThere is whether the one name path is there, read as look reads it, by an open of the name
// itself for its attributes alone that asks for identification, never os.Lstat, whose opens let a
// pipe's server reached by a link on the way act as the caller: nil where it is, an error matching
// fs.ErrNotExist where it is not.
func nameThere(path string) error {
	h, err := openName(path, windows.FILE_READ_ATTRIBUTES)
	if err != nil {
		return err
	}
	if err := windows.CloseHandle(h); err != nil {
		return &fs.PathError{Op: "close", Path: path, Err: err}
	}
	return nil
}

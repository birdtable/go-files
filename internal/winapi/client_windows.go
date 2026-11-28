package winapi

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Identify is the flag pair every CreateFile of go-files passes: SECURITY_SQOS_PRESENT with
// SECURITY_IDENTIFICATION, so where an open lands on a named pipe its server may learn who the
// caller is and act as it in nothing. Without it the system lets a pipe's server impersonate the
// client, which a server holding SeImpersonatePrivilege can act with. Static tracking is
// CreateFile's own when the pair is passed. The system reads SECURITY_SQOS_PRESENT as
// FILE_FLAG_OPEN_NO_RECALL too, the two being one bit, so an open with it leaves a file's data in
// remote storage, which no open of go-files' attributes or of a file trust reads asks to recall.
// trust and trust/trusttest, which may not import this package, pass the same pair by hand.
const Identify = windows.SECURITY_SQOS_PRESENT | windows.SECURITY_IDENTIFICATION

// CreateFile is windows.CreateFile with Identify added to flags, no security attributes and no
// template, the one CreateFile confine and appdirs call.
func CreateFile(name *uint16, access, share, disposition, flags uint32) (windows.Handle, error) {
	return windows.CreateFile(name, access, share, nil, disposition, flags|Identify, 0)
}

// identifyOnly is the SECURITY_QUALITY_OF_SERVICE OpenAt gives NtCreateFile: identification,
// with static tracking, the client's token taken once at the open.
func identifyOnly() *windows.SECURITY_QUALITY_OF_SERVICE {
	return &windows.SECURITY_QUALITY_OF_SERVICE{
		Length:              uint32(unsafe.Sizeof(windows.SECURITY_QUALITY_OF_SERVICE{})),
		ImpersonationLevel:  windows.SecurityIdentification,
		ContextTrackingMode: windows.SECURITY_STATIC_TRACKING,
	}
}

// fileAttributeTagInfo is FILE_ATTRIBUTE_TAG_INFO, which x/sys does not name.
type fileAttributeTagInfo struct {
	attributes, reparseTag uint32
}

// nameSurrogate is the bit of a reparse tag that marks a name for another file, a link or a
// junction, as IsReparseTagNameSurrogate reads it.
const nameSurrogate = 0x20000000

// OpenPath opens path for its attributes alone, which holds no other opener off, and a folder as a
// file. Its last name is opened where it is a reparse point, then, with follow and where that is a
// name surrogate, a link or a junction, opened again through it, as os.Stat opens a name. Every open
// passes Identify, so none lets a pipe's server act as the caller, where Go's own opens let it. A
// link on the way to the last name is followed. An error is an *fs.PathError, matching
// fs.ErrNotExist for a name not there.
func OpenPath(path string, follow bool) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := CreateFile(p, windows.FILE_READ_ATTRIBUTES, 0, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if err != nil {
		return nil, &fs.PathError{Op: "CreateFile", Path: path, Err: err}
	}
	if !follow {
		return os.NewFile(uintptr(h), path), nil
	}
	// A pipe or a character device keeps no reparse point, and may answer no query of one: as
	// os.Stat does, only a file on a disk is asked.
	ft, err := windows.GetFileType(h)
	switch {
	case err != nil:
		return nil, &fs.PathError{Op: "GetFileType", Path: path, Err: errors.Join(err, windows.CloseHandle(h))}
	case ft != windows.FILE_TYPE_DISK:
		return os.NewFile(uintptr(h), path), nil
	}
	var tag fileAttributeTagInfo
	err = windows.GetFileInformationByHandleEx(h, windows.FileAttributeTagInfo, (*byte)(unsafe.Pointer(&tag)), uint32(unsafe.Sizeof(tag))) //nolint:gosec // G103: the pointer is to tag, a FILE_ATTRIBUTE_TAG_INFO, for the one call
	switch {
	case err != nil:
		return nil, &fs.PathError{Op: "GetFileInformationByHandleEx", Path: path, Err: errors.Join(err, windows.CloseHandle(h))}
	case tag.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 || tag.reparseTag&nameSurrogate == 0:
		return os.NewFile(uintptr(h), path), nil
	}
	if err := windows.CloseHandle(h); err != nil {
		return nil, &fs.PathError{Op: "CloseHandle", Path: path, Err: err}
	}
	h, err = CreateFile(p, windows.FILE_READ_ATTRIBUTES, 0, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS)
	if err != nil {
		return nil, &fs.PathError{Op: "CreateFile", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// OpenFolder is os.Open of a folder by CreateFile with Identify, the Open a make by path is given:
// path opened to read, sharing read, write and delete, every link on the way and at its last name
// followed, as os.Open opens a folder, so the open lets no pipe's server reached on the way act
// as the caller. Through a link to a pipe on this host's own pipes share, \\localhost\pipe\<name>,
// the share's server opens the pipe at the level asked, so its server gets identification alone
// (TestALinkToTheLoopbackPipesShareGivesItsServerIdentificationAlone, read natively in CI run
// 46147851244). An error is an *fs.PathError.
func OpenFolder(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// Stat is os.Stat by OpenPath: the answer is read from a handle opened through path's last link, as
// os.Stat reads it, and is one os.SameFile compares, its volume and index read on the handle. It
// never asks GetFileAttributesEx, whose open of the name passes no quality of service.
func Stat(path string) (fs.FileInfo, error) { return stat(path, true) }

// Lstat is os.Lstat by OpenPath: the answer is of path's last name itself where it is a reparse point,
// and, as os.Lstat takes a path that ends in a separator, of what it leads to where path ends in
// one.
func Lstat(path string) (fs.FileInfo, error) {
	return stat(path, path != "" && os.IsPathSeparator(path[len(path)-1]))
}

func stat(path string, follow bool) (fs.FileInfo, error) {
	f, err := OpenPath(path, follow)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if cerr := f.Close(); err == nil && cerr != nil {
		return nil, cerr
	}
	if err != nil {
		return nil, err
	}
	return fi, nil
}

// symlinkRelative is SYMLINK_FLAG_RELATIVE, the flag of a link whose target is stored relative to
// the link's own folder.
const symlinkRelative = 1

// Readlink is os.Readlink by OpenPath: the target of the link or junction at path's last name,
// spelled as os.Readlink spells it (C:\dir, \\host\share\dir, the stored text of a relative link,
// \\?\Volume{...}\ for a volume), read from a handle opened on the name itself, so no open of
// path lets a pipe's server act as the caller. A reparse point of another kind is answered as
// os.Readlink answers it, with an error matching fs.ErrNotExist. An error is an *fs.PathError.
func Readlink(path string) (string, error) {
	f, err := OpenPath(path, false)
	if err != nil {
		return "", err
	}
	var buf [windows.MAXIMUM_REPARSE_DATA_BUFFER_SIZE]byte
	var n uint32
	err = windows.DeviceIoControl(windows.Handle(f.Fd()), windows.FSCTL_GET_REPARSE_POINT, nil, 0, &buf[0], windows.MAXIMUM_REPARSE_DATA_BUFFER_SIZE, &n, nil)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", &fs.PathError{Op: "readlink", Path: path, Err: err}
	}
	target, err := linkTarget(buf[:n])
	if err != nil {
		return "", &fs.PathError{Op: "readlink", Path: path, Err: err}
	}
	return target, nil
}

// linkTarget is the target b holds, a REPARSE_DATA_BUFFER: a link's or a junction's substitute
// name, a relative link's as stored and any other as a DOS path, as os.Readlink reads it.
func linkTarget(b []byte) (string, error) {
	le := binary.LittleEndian
	if len(b) < 16 {
		return "", fmt.Errorf("the reparse data is %d bytes, too short for a header", len(b))
	}
	var names []byte
	relative := false
	switch tag := le.Uint32(b); tag {
	case windows.IO_REPARSE_TAG_SYMLINK:
		if len(b) < 20 {
			return "", fmt.Errorf("the link's reparse data is %d bytes, too short for its flags", len(b))
		}
		names, relative = b[20:], le.Uint32(b[16:])&symlinkRelative != 0
	case windows.IO_REPARSE_TAG_MOUNT_POINT:
		names = b[16:]
	default:
		return "", fmt.Errorf("a reparse point of tag %#x, neither a link nor a junction: %w", tag, fs.ErrNotExist)
	}
	off, size := int(le.Uint16(b[8:])), int(le.Uint16(b[10:]))
	if off%2 != 0 || size%2 != 0 || off+size > len(names) {
		return "", fmt.Errorf("the substitute name at %d, %d bytes, lies outside the %d bytes of names", off, size, len(names))
	}
	u := make([]uint16, size/2)
	for i := range u {
		u[i] = le.Uint16(names[off+2*i:])
	}
	s := windows.UTF16ToString(u)
	if relative {
		return s, nil
	}
	return dosTarget(s), nil
}

// dosTarget is s, an NT path, as os.Readlink spells it: \??\C:\dir as C:\dir, \??\UNC\host\share
// as \\host\share, any other \??\ name as \\?\ and the name, and anything else as it is.
func dosTarget(s string) string {
	rest, ok := strings.CutPrefix(s, `\??\`)
	switch {
	case !ok:
		return s
	case len(rest) >= 2 && rest[1] == ':':
		return rest
	case strings.HasPrefix(rest, `UNC\`):
		return `\\` + rest[4:]
	}
	return `\\?\` + rest
}

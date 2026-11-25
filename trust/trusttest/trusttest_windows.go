package trusttest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The groups Loosen lets in on Windows.
const (
	users    = "S-1-5-32-545"
	everyone = "S-1-1-0"
)

// entry is the allow entry that lets who in: Users or Everyone, with the rights to read or to
// write a file, on the name alone.
func entry(who Who) (windows.EXPLICIT_ACCESS, error) {
	sid, rights := everyone, uint32(windows.FILE_GENERIC_WRITE)
	if who == GroupReads || who == GroupWrites {
		sid = users
	}
	if who == GroupReads || who == OthersRead {
		rights = windows.FILE_GENERIC_READ
	}
	s, err := windows.StringToSid(sid)
	if err != nil {
		return windows.EXPLICIT_ACCESS{}, err
	}
	return windows.EXPLICIT_ACCESS{
		AccessPermissions: windows.ACCESS_MASK(rights),
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(s),
		},
	}, nil
}

// loosen adds who's allow entry to path's access list, and is the put that sets the list it had.
// A null list lets everyone do anything already, and is left as it is.
func loosen(path string, who Who) (func() error, error) {
	e, err := entry(who)
	if err != nil {
		return nil, err
	}
	old, err := listOf(path)
	if err != nil {
		return nil, err
	}
	if old.dacl == nil {
		return func() error { return nil }, nil
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{e}, old.dacl)
	if err != nil {
		return nil, fmt.Errorf("SetEntriesInAcl: %w", err)
	}
	if err := setList(path, acl, old.protected); err != nil {
		return nil, err
	}
	return old.put(path), nil
}

// deny gives path an empty access list that takes nothing from the folder above, and is the put
// that sets the list it had.
func deny(path string) (func() error, error) {
	old, err := listOf(path)
	if err != nil {
		return nil, err
	}
	// An empty list read from its SDDL: ACLFromEntries(nil, nil) panics in x/sys, which reads the
	// size of the null list SetEntriesInAcl answers for no entries.
	sd, err := windows.SecurityDescriptorFromString("D:P")
	if err != nil {
		return nil, fmt.Errorf("an empty list: %w", err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return nil, fmt.Errorf("an empty list: %w", err)
	}
	if acl == nil {
		return nil, errors.New("an empty list: D:P read as a null list")
	}
	if err := setList(path, acl, true); err != nil {
		return nil, err
	}
	return old.put(path), nil
}

// list is a name's access list as it was read: nil for a null or absent one, which lets
// everyone do anything, and whether it takes nothing from the folder above. sd is the descriptor
// read, which holds dacl's bytes and is what put sets back.
type list struct {
	sd        *windows.SECURITY_DESCRIPTOR
	dacl      *windows.ACL
	protected bool
}

// put is the put that sets l back on path as it was read, by SetKernelObjectSecurity, which
// writes the list and its control bits as given: SetSecurityInfo marks an inheriting list
// auto-inherited (AI), which the list found need not have been, so the list after would not be
// the one found.
func (l list) put(path string) func() error {
	return func() error {
		h, err := reopen(path)
		if err != nil {
			return err
		}
		defer windows.CloseHandle(h) //nolint:errcheck // the change is made or refused by SetKernelObjectSecurity
		info := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.UNPROTECTED_DACL_SECURITY_INFORMATION)
		if l.protected {
			info = windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION
		}
		if err := windows.SetKernelObjectSecurity(h, info, l.sd); err != nil {
			return &fs.PathError{Op: "SetKernelObjectSecurity", Path: path, Err: err}
		}
		return nil
	}
}

// reopen opens path, following a link, with READ_CONTROL and WRITE_DAC alone, the two rights its
// owner keeps under any list, to put its list back. CreateFile adds SYNCHRONIZE and
// FILE_READ_ATTRIBUTES to what it is asked, which Deny's empty list refuses the owner too, so
// path is opened by NtCreateFile beneath its folder, asking no synchronous I/O, which would need
// SYNCHRONIZE.
func reopen(path string) (windows.Handle, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	d, err := windows.UTF16PtrFromString(filepath.Dir(abs))
	if err != nil {
		return 0, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	dir, err := windows.CreateFile(d, windows.FILE_TRAVERSE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0, &fs.PathError{Op: "open", Path: filepath.Dir(abs), Err: err}
	}
	defer windows.CloseHandle(dir) //nolint:errcheck // a folder only opened beneath
	name, err := windows.NewNTUnicodeString(filepath.Base(abs))
	if err != nil {
		return 0, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	attrs := windows.OBJECT_ATTRIBUTES{Length: uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})), RootDirectory: dir, ObjectName: name, Attributes: windows.OBJ_CASE_INSENSITIVE}
	var h windows.Handle
	if err := windows.NtCreateFile(&h, windows.READ_CONTROL|windows.WRITE_DAC, &attrs, &windows.IO_STATUS_BLOCK{}, nil, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN, windows.FILE_OPEN_FOR_BACKUP_INTENT, 0, 0); err != nil {
		if st := windows.NTStatus(0); errors.As(err, &st) {
			err = st.Errno()
		}
		return 0, &fs.PathError{Op: "NtCreateFile", Path: path, Err: err}
	}
	return h, nil
}

// listOf is the access list of path.
func listOf(path string) (list, error) {
	h, err := open(path, windows.READ_CONTROL)
	if err != nil {
		return list{}, err
	}
	defer windows.CloseHandle(h) //nolint:errcheck // a handle only read
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return list{}, &fs.PathError{Op: "GetSecurityInfo", Path: path, Err: err}
	}
	control, _, err := sd.Control()
	if err != nil {
		return list{}, &fs.PathError{Op: "GetSecurityDescriptorControl", Path: path, Err: err}
	}
	dacl, _, err := sd.DACL()
	if err != nil && !errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND) {
		return list{}, &fs.PathError{Op: "GetSecurityDescriptorDacl", Path: path, Err: err}
	}
	return list{sd: sd, dacl: dacl, protected: control&windows.SE_DACL_PROTECTED != 0}, nil
}

// setList sets acl as path's access list, nil a null one, taking nothing from the folder above
// where protected.
func setList(path string, acl *windows.ACL, protected bool) error {
	h, err := open(path, windows.READ_CONTROL|windows.WRITE_DAC)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h) //nolint:errcheck // the change is made or refused by SetSecurityInfo
	info := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.UNPROTECTED_DACL_SECURITY_INFORMATION)
	if protected {
		info = windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION
	}
	if err := windows.SetSecurityInfo(h, windows.SE_FILE_OBJECT, info, nil, nil, acl, nil); err != nil {
		return &fs.PathError{Op: "SetSecurityInfo", Path: path, Err: err}
	}
	return nil
}

// open opens the file or folder path names, following a link, with access, and refuses a name
// on a volume that keeps no access lists.
func open(path string, access uint32) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	var flags uint32
	err = windows.GetVolumeInformationByHandle(h, nil, 0, nil, nil, &flags, nil, 0)
	switch {
	case err != nil:
		err = &fs.PathError{Op: "GetVolumeInformationByHandle", Path: path, Err: err}
	case flags&windows.FILE_PERSISTENT_ACLS == 0:
		err = errNoLists
	}
	if err != nil {
		return 0, errors.Join(err, windows.CloseHandle(h))
	}
	return h, nil
}

// kept is the mode os.Chmod leaves a name given want on Windows, which keeps want's owner write
// bit alone, as the read-only attribute: 0o666 with it, 0o444 without, a folder adding 0o111.
func kept(want fs.FileMode, folder bool) fs.FileMode {
	m := fs.FileMode(0o444)
	if want&0o200 != 0 {
		m = 0o666
	}
	if folder {
		m |= 0o111
	}
	return m
}

// samePath is whether real, path with its links resolved, names path: alike but for case, which
// Windows does not tell apart and EvalSymlinks gives as the folder holds it.
func samePath(real, path string) bool { return strings.EqualFold(real, path) }

// openWrite opens path to write, and closes it at once, writing nothing.
func openWrite(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0) //nolint:gosec // G304: a candidate of SystemFile's, never written
	if err != nil {
		return err
	}
	return f.Close()
}

// openRead opens path to read: on Windows an open never waits for a pipe's other end.
func openRead(path string) (*os.File, error) {
	return os.Open(path) //nolint:gosec // G304: the path the caller names
}

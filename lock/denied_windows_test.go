package lock

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// The rights the denying lists grant this account, and nothing beyond: a file's generic read, and
// a folder's generic read and execute, which list and traverse it and add nothing to it.
const (
	fileGenericRead      = 0x120089
	folderReadAndExecute = 0x1200a9
)

// onlyGrants is a self-relative security descriptor whose protected access list grants this
// account mask and nobody anything else, with no owner or group, which the make takes from the
// token. It is built by the standard library alone, which lock's tests are held to: the revision,
// the control (self-relative, a list present and protected) and the list's offset, then the list's
// header and its one allow entry naming this account's SID.
//
//nolint:gosec // G103: the SID is copied out of the token's buffer by the length the system gives
func onlyGrants(t *testing.T, mask uint32) []byte {
	t.Helper()
	tok, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tok.Close(); err != nil {
			t.Error(err)
		}
	}()
	user, err := tok.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	n := syscall.GetLengthSid(user.User.Sid)
	sid := unsafe.Slice((*byte)(unsafe.Pointer(user.User.Sid)), n)
	const header, aclHeader, aceHeader = 20, 8, 8
	aceSize := aceHeader + len(sid)
	sd := make([]byte, header+aclHeader+aceSize)
	sd[0] = 1                                                   // the descriptor's revision
	binary.LittleEndian.PutUint16(sd[2:], 0x8000|0x1000|0x0004) // self-relative, list protected, list present
	binary.LittleEndian.PutUint32(sd[16:], header)              // the list's offset; no owner, group or audit list
	acl := sd[header:]
	acl[0] = 2                                                        // the list's revision
	binary.LittleEndian.PutUint16(acl[2:], uint16(aclHeader+aceSize)) // the list's size
	binary.LittleEndian.PutUint16(acl[4:], 1)                         // one entry
	ace := acl[aclHeader:]
	ace[0] = 0                                              // allow, not inherited, inheriting nothing
	binary.LittleEndian.PutUint16(ace[2:], uint16(aceSize)) // the entry's size
	binary.LittleEndian.PutUint32(ace[4:], mask)            // the rights it grants
	copy(ace[aceHeader:], sid)
	return sd
}

// makeDenied makes path, a file or a folder, with the list onlyGrants gives mask. Its folder,
// t.TempDir's, still grants this account the deletion of what is in it, so the cleanup removes it.
//
//nolint:gosec // G103: the descriptor is handed to the system by its address, and kept alive past the make
func makeDenied(t *testing.T, path string, folder bool, mask uint32) {
	t.Helper()
	sd := onlyGrants(t, mask)
	sa := &syscall.SecurityAttributes{SecurityDescriptor: uintptr(unsafe.Pointer(&sd[0]))}
	sa.Length = uint32(unsafe.Sizeof(*sa))
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	if folder {
		err = syscall.CreateDirectory(name, sa)
	} else {
		var h syscall.Handle
		h, err = syscall.CreateFile(name, syscall.GENERIC_READ, 0, sa, syscall.CREATE_NEW, syscall.FILE_ATTRIBUTE_NORMAL, 0)
		if err == nil {
			err = syscall.CloseHandle(h)
		}
	}
	runtime.KeepAlive(sd)
	if err != nil {
		t.Fatalf("make %s with a list that grants 0x%x alone: %v", path, mask, err)
	}
}

// On Windows a lock file whose access list grants this account read and not write is refused by
// Take and by Wait at once, matching fs.ErrPermission, naming the file and never as Held; and a
// missing lock file in a folder whose list denies this account the make is refused so too, with
// nothing made in the folder, the probe's throwaway name included. Each is made by an access list,
// not a mode bit, which Windows does not have.
//
// What it costs when this fails: a program that may not write its lock is told another run holds
// it and waits for one that never comes, or takes a lock through a handle it cannot write its
// note by.
func TestOnWindowsALockFileOrFolderWhoseListDeniesThisAccountWriteIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	there := filepath.Join(dir, "app.lock")
	makeDenied(t, there, false, fileGenericRead)
	shut := filepath.Join(dir, "shut")
	makeDenied(t, shut, true, folderReadAndExecute)
	missing := filepath.Join(shut, "app.lock")
	for what, path := range map[string]string{"a lock file there": there, "a missing lock file in a folder": missing} {
		for name, try := range map[string]func() (*Lock, error){
			"Take": func() (*Lock, error) { return Take(path, path+".holder") },
			"Wait": func() (*Lock, error) { return Wait(t.Context(), path, path+".holder") },
		} {
			l, err := try()
			if l != nil {
				t.Errorf("%s of %s whose list denies this account write took the lock", name, what)
				if err := l.Release(); err != nil {
					t.Error(err)
				}
			}
			if !errors.Is(err, fs.ErrPermission) || IsHeld(err) || !strings.Contains(err.Error(), path) {
				t.Errorf("%s of %s whose list denies this account write: %v, and want fs.ErrPermission naming the file, not Held", name, what, err)
			}
		}
	}
	if names, err := os.ReadDir(shut); err != nil || len(names) != 0 {
		t.Errorf("the folder whose list denies the make holds %v (%v), and want nothing made", names, err)
	}
	if _, err := os.Lstat(there + ".holder"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a refused Take left a note beside the lock file: %v", err)
	}
}

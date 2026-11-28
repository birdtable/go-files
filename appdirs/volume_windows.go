package appdirs

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/birdtable/go-files/internal/winapi"
)

const (
	// volumeNameNT is GetFinalPathNameByHandle's VOLUME_NAME_NT, from fileapi.h: the path as the NT
	// object manager names it, its device first.
	volumeNameNT = 0x2
	// volumeNameGUID is GetFinalPathNameByHandle's VOLUME_NAME_GUID: the path below its volume's
	// \\?\Volume{GUID}\ name.
	volumeNameGUID = 0x1
	// syncRootManager is the key, under HKEY_LOCAL_MACHINE, where Windows registers each Cloud Files
	// sync root: a key a root, whose UserSyncRoots key holds the root's path in a value an account.
	syncRootManager = `SOFTWARE\Microsoft\Windows\CurrentVersion\Explorer\SyncRootManager`
)

// askVolume asks the volume of path, made absolute as abs, opened with links followed, its NT
// path and file system name and its drive type, then, on a volume not served over the network, the
// Cloud Files marks of its final path and each folder above it, and the sync roots registered.
func askVolume(path, abs string) (VolumeInfo, error) {
	p, err := windows.UTF16PtrFromString(abs)
	if err != nil {
		return VolumeInfo{}, &fs.PathError{Op: opVolume, Path: path, Err: err}
	}
	h, err := winapi.CreateFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS)
	if err != nil {
		return VolumeInfo{}, &fs.PathError{Op: opVolume, Path: path, Err: err}
	}
	v, err := askHandle(h)
	if cerr := windows.CloseHandle(h); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		return VolumeInfo{}, &fs.PathError{Op: opVolume, Path: path, Err: err}
	}
	return v, nil
}

// askHandle is askVolume's answer for h, the handle of its path, refused where it is a pipe or a
// device. The drive type is asked of the volume's root, read from h, never of the path.
func askHandle(h windows.Handle) (VolumeInfo, error) {
	if err := onVolume(h); err != nil {
		return VolumeInfo{}, err
	}
	var nt [windows.MAX_LONG_PATH]uint16
	n, err := windows.GetFinalPathNameByHandle(h, &nt[0], uint32(len(nt)), volumeNameNT)
	if err != nil {
		return VolumeInfo{}, err
	}
	var name [windows.MAX_PATH + 1]uint16
	if err := windows.GetVolumeInformationByHandle(h, nil, 0, nil, nil, nil, &name[0], uint32(len(name))); err != nil {
		return VolumeInfo{}, err
	}
	drive := uint32(windows.DRIVE_UNKNOWN)
	if root, err := volumeRoot(h); err == nil {
		drive = windows.GetDriveType(root)
	}
	ntPath := windows.UTF16ToString(nt[:min(int(n), len(nt))])
	sync := syncCannotTell
	if drive != driveRemote && !strings.HasPrefix(ntPath, `\Device\Mup\`) {
		sync = cloudSync(h)
	}
	return windowsVolume(ntPath, windows.UTF16ToString(name[:]), drive, sync), nil
}

// onVolume refuses h unless it is open on a file or a folder of a volume: a pipe or a device, where
// a path's links lead to one, is no path on a volume, and nothing is asked of it by name.
func onVolume(h windows.Handle) error {
	ft, err := windows.GetFileType(h)
	if err != nil {
		return err
	}
	if ft != windows.FILE_TYPE_DISK && ft != windows.FILE_TYPE_PIPE && ft != windows.FILE_TYPE_CHAR {
		return fmt.Errorf("it leads to a pipe or a device, not a file or a folder on a volume: %w", fs.ErrInvalid)
	}
	return nil
}

// volumeRoot is the root of the volume h is open on, \\?\Volume{GUID}\, read from the handle
// (VOLUME_NAME_GUID), never from a path: no account but an administrator makes a reparse point at
// a volume's root, so a question asked of it by name reaches that volume. A volume with no GUID, a
// share among them, is an error.
func volumeRoot(h windows.Handle) (*uint16, error) {
	var buf [windows.MAX_LONG_PATH]uint16
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), volumeNameGUID)
	if err != nil {
		return nil, err
	}
	final := windows.UTF16ToString(buf[:min(int(n), len(buf))])
	end := strings.Index(final, `}\`)
	if !strings.HasPrefix(final, `\\?\Volume{`) || end < 0 {
		return nil, fmt.Errorf("the final path %s names no volume by its GUID: %w", final, fs.ErrInvalid)
	}
	return windows.UTF16PtrFromString(final[:end+2])
}

// cloudSync is what the Cloud Files marks say of h's final path, a path on a drive letter, and
// syncCannotTell for any other final path, or one that cannot be read.
func cloudSync(h windows.Handle) syncAnswer {
	dos, err := finalPath(h)
	if err != nil || len(dos) < 3 || dos[1] != ':' || dos[2] != '\\' {
		return syncCannotTell
	}
	roots, err := syncRoots()
	return windowsSync(dos, reparseTag, roots, err)
}

// reparseTag is the reparse tag of the name p, a DOS path, opened with no link followed, or 0 where
// it is no reparse point.
func reparseTag(p string) (uint32, error) {
	name, err := windows.UTF16PtrFromString(`\\?\` + p)
	if err != nil {
		return 0, err
	}
	h, err := winapi.CreateFile(name, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if err != nil {
		return 0, err
	}
	// FILE_ATTRIBUTE_TAG_INFO: the name's attributes and its reparse tag.
	var info struct{ attributes, tag uint32 }
	err = windows.GetFileInformationByHandleEx(h, windows.FileAttributeTagInfo, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))) //nolint:gosec // G103: the pointer is to info, a FILE_ATTRIBUTE_TAG_INFO, for the one call
	if cerr := windows.CloseHandle(h); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil || info.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		return 0, err
	}
	return info.tag, nil
}

// syncRoots is the path of each Cloud Files sync root registered for any account, read from the
// 64-bit registry, and none where no root was ever registered. A value that is not a string is
// passed over.
func syncRoots() ([]string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, syncRootManager, registry.ENUMERATE_SUB_KEYS|registry.WOW64_64KEY)
	if errors.Is(err, registry.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var roots []string
	subs, err := k.ReadSubKeyNames(-1)
	for _, s := range subs {
		if err != nil {
			break
		}
		var got []string
		got, err = userSyncRoots(k, s)
		roots = append(roots, got...)
	}
	return roots, errors.Join(err, k.Close())
}

// userSyncRoots is each path the UserSyncRoots key of the sync root sub, a key of parent, holds.
func userSyncRoots(parent registry.Key, sub string) ([]string, error) {
	k, err := registry.OpenKey(parent, sub+`\UserSyncRoots`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if errors.Is(err, registry.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var roots []string
	names, err := k.ReadValueNames(-1)
	for _, n := range names {
		if err != nil {
			break
		}
		s, _, gerr := k.GetStringValue(n)
		switch {
		case gerr == nil:
			roots = append(roots, s)
		case !errors.Is(gerr, registry.ErrUnexpectedType):
			err = gerr
		}
	}
	return roots, errors.Join(err, k.Close())
}

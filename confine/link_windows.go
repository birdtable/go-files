package confine

// The link on Windows: a junction to a folder, or a symbolic link to a file, made by handle beneath
// the root. The README says how.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/birdtable/go-files/internal/winapi"
)

// link makes at a junction to target. setJunction is the system's set; a test passes another.
func link(root *os.Root, target, at string) error {
	return linkWith(root, target, at, folderLink(setJunction))
}

// linkFile makes at a symbolic link to the file target. setSymlink is the system's set; a test
// passes another.
func linkFile(root *os.Root, target, at string) error {
	return linkWith(root, target, at, fileLink(setSymlink))
}

// linkKindOf is what linkWith makes: the reparse data for a target, the NtCreateFile option that
// makes at a folder or a file, and the set of the data on its handle.
type linkKindOf struct {
	data    func(target string) ([]byte, error)
	options uint32
	set     func(windows.Handle, []byte) error
}

// folderLink is a junction, set by set.
func folderLink(set func(windows.Handle, []byte) error) linkKindOf {
	return linkKindOf{data: junctionData, options: windows.FILE_DIRECTORY_FILE, set: set}
}

// fileLink is a symbolic link to a file, set by set.
func fileLink(set func(windows.Handle, []byte) error) linkKindOf {
	return linkKindOf{data: symlinkData, options: windows.FILE_NON_DIRECTORY_FILE, set: set}
}

// linkWith holds at's folders from the root's own handle down, refusing a reparse point among
// them, makes at, a folder or a file as kind says, in the last by NtCreateFile with FILE_CREATE, so
// it is new and no name there is followed, and sets kind's reparse data on its own handle. A name
// the set refuses is removed by that handle, so the name removed is the one made.
func linkWith(root *os.Root, target, at string, kind linkKindOf) (err error) {
	data, err := kind.data(target)
	if err != nil {
		return &os.LinkError{Op: opLink, Old: target, New: at, Err: err}
	}
	top, err := root.Open(".")
	if err != nil {
		return err
	}
	defer closing(top, &err)
	dirs, err := hold(windows.Handle(top.Fd()), at, opWalk)
	if err != nil {
		return err
	}
	defer closing(dirs, &err)
	f, err := ntOpen(dirs.in, filepath.Base(at), filepath.ToSlash(at), opLink, winapi.How{
		Access:      windows.FILE_WRITE_DATA | windows.FILE_WRITE_ATTRIBUTES | windows.DELETE,
		Share:       windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE,
		Disposition: windows.FILE_CREATE,
		Options:     kind.options,
	})
	if errors.Is(err, fs.ErrExist) {
		return madeErr(at, fs.ErrExist)
	}
	if err != nil {
		return err
	}
	defer closing(f, &err)
	if err := kind.set(f.h, data); err != nil {
		return &os.LinkError{Op: opLink, Old: target, New: at, Err: errors.Join(err, removeHeld(f.h, at))}
	}
	return nil
}

// absoluteLinks: a link CopyTree makes holds an absolute target, as a junction holds no other.
const absoluteLinks = true

// fits refuses, for LinkFolder's junction and LinkFile's link alike, a target the check and the
// link would read as two, one off this machine's disks, and one whose mount point data would not
// fit. os.Stat reads the target by the Win32 name rules, which strip a dot or a space that ends a
// component, and the link holds it as an NT name, which keeps them, so such a target is refused,
// as it is for at. A target must be on a drive letter of a fixed or removable disk of this machine:
// not a share, which a junction does not reach, nor a letter mapped to one, nor a letter made by
// subst or by DefineDosDevice with a raw target, which holds only in the session that made it.
func fits(target string) error {
	if strings.HasPrefix(target, `\\`) {
		return fmt.Errorf("the target is not on a disk of this machine, and a link made here names a target on one only: %w", fs.ErrInvalid)
	}
	if trailed(target, string(filepath.Separator)) {
		return fmt.Errorf("a component of the target ends in a dot or a space, which the Win32 name rules strip, so the target checked would not be the one the link holds: %w", fs.ErrInvalid)
	}
	if err := localDrive(filepath.VolumeName(target)); err != nil {
		return err
	}
	_, err := junctionData(target)
	return err
}

// localDrive refuses vol unless it is a drive letter of a fixed or removable disk, by
// GetDriveType of its root, that names a disk's volume as every logon session sees it (dosName).
func localDrive(vol string) error {
	if len(vol) != 2 || vol[1] != ':' {
		return fmt.Errorf("the target is not on a drive letter: %w", fs.ErrInvalid)
	}
	top, err := windows.UTF16PtrFromString(vol + `\`)
	if err != nil {
		return err
	}
	if kind := windows.GetDriveType(top); kind != windows.DRIVE_FIXED && kind != windows.DRIVE_REMOVABLE {
		return fmt.Errorf("the target's drive %s is not a fixed or removable disk of this machine (drive type %d), and a link made here names a target on one only: %w", vol, kind, fs.ErrInvalid)
	}
	return dosName(vol, dosDevice, mountedVolume)
}

// dosName refuses vol unless device, QueryDosDevice, names a device for it and not a folder in
// the object manager's \??\ form, which subst makes, and volume (mountedVolume) finds the mount
// manager's volume at its root and the mount manager lists the root as one of that volume's own.
// A letter DefineDosDevice made with DDD_RAW_TARGET_PATH names a device, or a folder on one, in
// this logon session only: the mount manager does not know one to a folder, and
// does not list one to a whole volume among that volume's paths. A global letter the
// mount manager did not make, one Session Manager\DOS Devices or a driver defines, is not listed
// either, and is refused though every session sees it. Every refusal matches
// fs.ErrInvalid, a failed look among them.
func dosName(vol string, device func(vol string) (string, error), volume func(top string) error) error {
	dev, err := device(vol)
	if err != nil {
		return fmt.Errorf("the target's drive %s could not be looked up, so it is not known to be a disk of this machine: %w: %w", vol, fs.ErrInvalid, err)
	}
	if strings.HasPrefix(dev, `\??\`) {
		return fmt.Errorf("the target's drive %s is a folder subst named, which other logon sessions do not see: %w", vol, fs.ErrInvalid)
	}
	if err := volume(vol + `\`); err != nil {
		return fmt.Errorf("the target's drive %s is not among the paths the mount manager mounted a volume at, as a letter DefineDosDevice made with a raw target is not, nor one Session Manager or a driver defines: %w: %w", vol, fs.ErrInvalid, err)
	}
	return nil
}

// dosDevice is what QueryDosDevice says vol names: its first target, a device or a \??\ name.
func dosDevice(vol string) (string, error) {
	name, err := windows.UTF16PtrFromString(vol)
	if err != nil {
		return "", err
	}
	var dev [windows.MAX_PATH]uint16
	n, err := windows.QueryDosDevice(name, &dev[0], windows.MAX_PATH)
	if err != nil {
		return "", err
	}
	return windows.UTF16ToString(dev[:n]), nil
}

// mountedVolume is nil when the mount manager names a volume at top, a drive's root, and lists
// top among that volume's own paths. GetVolumeNameForVolumeMountPoint alone answers for a letter
// DefineDosDevice made raw to a whole volume, as it resolves the letter to its device; the list
// names only the letters and folders the mount manager mounted the volume at.
func mountedVolume(top string) error {
	p, err := windows.UTF16PtrFromString(top)
	if err != nil {
		return err
	}
	var name [50]uint16 // \\?\Volume{GUID}\ is 49 characters, and its NUL
	if err := windows.GetVolumeNameForVolumeMountPoint(p, &name[0], uint32(len(name))); err != nil {
		return err
	}
	return mountedAt(windows.UTF16ToString(name[:]), top)
}

// mountedAt is nil when the mount manager lists at, a drive's root or a folder ending in a
// separator, among the paths of volume, a name \\?\Volume{GUID}\.
func mountedAt(volume, at string) error {
	name, err := windows.UTF16PtrFromString(volume)
	if err != nil {
		return err
	}
	size := uint32(windows.MAX_PATH)
	paths := make([]uint16, size)
	var n uint32
	err = windows.GetVolumePathNamesForVolumeName(name, &paths[0], size, &n)
	if errors.Is(err, windows.ERROR_MORE_DATA) {
		size = n
		paths = make([]uint16, size)
		err = windows.GetVolumePathNamesForVolumeName(name, &paths[0], size, &n)
	}
	if err != nil {
		return err
	}
	var listed []string
	for s := range strings.SplitSeq(string(utf16.Decode(paths[:n])), "\x00") { // each path ends in a NUL, and the list in another
		if s == "" {
			continue
		}
		if strings.EqualFold(s, at) {
			return nil
		}
		listed = append(listed, s)
	}
	return fmt.Errorf("the mount manager mounted %s at %q, not at %s", volume, listed, at)
}

// junctionData is the mount point reparse data of a junction to target, absolute.
func junctionData(target string) ([]byte, error) {
	return reparseData(windows.IO_REPARSE_TAG_MOUNT_POINT, target)
}

// symlinkData is the reparse data of a symbolic link to target, absolute, its flags 0.
func symlinkData(target string) ([]byte, error) {
	return reparseData(windows.IO_REPARSE_TAG_SYMLINK, target)
}

// reparseData is the reparse data of tag, a junction's or a symbolic link's, to target: the tag,
// the length of what follows the header, a reserved word, the substitute name's offset and length,
// the print name's offset and length, a symbolic link's flags, then the substitute name (target in
// the object manager's \??\ form) and the print name (target), each ended by a NUL. Each is
// converted as os.Stat converts a name, so a name Go holds as WTF-8, an unpaired surrogate in it,
// is the same name here. Data longer than the system takes, MAXIMUM_REPARSE_DATA_BUFFER_SIZE with
// its header, is refused, and every length in it then fits its 16 bits.
func reparseData(tag uint32, target string) ([]byte, error) {
	sub, err := wide(`\??\` + target)
	if err != nil {
		return nil, err
	}
	shown, err := wide(target)
	if err != nil {
		return nil, err
	}
	head := 16
	if tag == windows.IO_REPARSE_TAG_SYMLINK {
		head += 4 // the flags
	}
	size := head + 2*(len(sub)+1+len(shown)+1)
	if size > windows.MAXIMUM_REPARSE_DATA_BUFFER_SIZE {
		return nil, fmt.Errorf("the target is too long a path for a link's %d bytes: %w", windows.MAXIMUM_REPARSE_DATA_BUFFER_SIZE, fs.ErrInvalid)
	}
	le := binary.LittleEndian
	data := le.AppendUint32(make([]byte, 0, size), tag)
	for _, n := range []int{size - 8, 0, 0, 2 * len(sub), 2 * (len(sub) + 1), 2 * len(shown)} {
		data = le.AppendUint16(data, uint16(n)) //nolint:gosec // G115: size is at most 16 KiB, as checked above
	}
	if head > 16 {
		data = le.AppendUint32(data, 0) // an absolute target
	}
	for _, name := range [][]uint16{sub, shown} {
		for _, u := range name {
			data = le.AppendUint16(data, u)
		}
		data = le.AppendUint16(data, 0)
	}
	return data, nil
}

// setJunction sets the empty folder open at h to be the junction data says, with
// FSCTL_SET_REPARSE_POINT.
func setJunction(h windows.Handle, data []byte) error {
	var got uint32
	return windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &data[0], uint32(len(data)), nil, 0, &got, nil) //nolint:gosec // G115: data is at most 16 KiB, as reparseData checks
}

// setSymlink sets the empty file open at h to be the symbolic link data says, as setJunction
// sets a junction.
func setSymlink(h windows.Handle, data []byte) error {
	return symlinkErr(setJunction(h, data))
}

// symlinkErr is the system's answer err to the set of a symbolic link: its refusal for want of
// the privilege is ErrNoFileLink.
func symlinkErr(err error) error {
	if errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) {
		return fmt.Errorf("this account may not make a symbolic link (%s): %w", err.Error(), ErrNoFileLink)
	}
	return err
}

// removeHeld marks the name open at h, the one linkWith made, to be removed when h closes.
func removeHeld(h windows.Handle, at string) error {
	remove := uint8(1) // FILE_DISPOSITION_INFO's DeleteFile, one BOOLEAN
	if err := windows.SetFileInformationByHandle(h, windows.FileDispositionInfo, &remove, uint32(unsafe.Sizeof(remove))); err != nil {
		return &fs.PathError{Op: opLink, Path: filepath.ToSlash(at), Err: fmt.Errorf("the name made for the link is left there: %w", err)}
	}
	return nil
}

// wide is name in UTF-16, as os.Stat converts it, with no NUL at the end. A NUL in name is
// refused.
func wide(name string) ([]uint16, error) {
	w, err := windows.UTF16FromString(name)
	if err != nil {
		return nil, fmt.Errorf("the target holds a NUL: %w", fs.ErrInvalid)
	}
	return w[:len(w)-1], nil
}

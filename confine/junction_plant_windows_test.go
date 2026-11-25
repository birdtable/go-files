package confine

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// plantJunction makes link a junction, a mount point reparse point, to the folder target, which
// any account may make with no privilege.
func plantJunction(t *testing.T, target, link string) {
	t.Helper()
	abs, err := filepath.Abs(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := plantRawJunction(link, `\??\`+abs, abs); err != nil {
		t.Fatal(err)
	}
}

// plantRawJunction makes link, a new folder, a junction whose substitute name is sub and whose
// print name is shown, as written, with no cleaning, and answers the system's error.
func plantRawJunction(link, sub, shown string) error {
	return plantReparse(link, func(h windows.Handle) error { return setMountPointRaw(h, sub, shown) })
}

// plantReparse makes link a new folder and runs set on it, open by handle with its reparse point
// not followed, and answers set's error.
func plantReparse(link string, set func(windows.Handle) error) error {
	if err := os.Mkdir(link, 0o755); err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(name, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return set(h)
}

// setMountPoint makes the empty folder open at h a junction to the folder target, with
// FSCTL_SET_REPARSE_POINT, and answers the system's error.
func setMountPoint(t *testing.T, h windows.Handle, target string) error {
	t.Helper()
	abs, err := filepath.Abs(target)
	if err != nil {
		t.Fatal(err)
	}
	return setMountPointRaw(h, `\??\`+abs, abs)
}

// setMountPointRaw makes the empty folder open at h a mount point whose substitute name is sub
// and whose print name is shown, with FSCTL_SET_REPARSE_POINT, and answers the system's error.
func setMountPointRaw(h windows.Handle, sub, shown string) error {
	s := utf16.Encode([]rune(sub))
	p := utf16.Encode([]rune(shown))
	// The path buffer holds the substitute name and the print name, each ending in a NUL.
	names := make([]uint16, 0, len(s)+len(p)+2)
	names = append(append(append(append(names, s...), 0), p...), 0)
	data := make([]byte, 0, 8+2*len(names))
	data = binary.LittleEndian.AppendUint16(data, 0) // the substitute name's offset
	data = binary.LittleEndian.AppendUint16(data, uint16(2*len(s)))
	data = binary.LittleEndian.AppendUint16(data, uint16(2*(len(s)+1)))
	data = binary.LittleEndian.AppendUint16(data, uint16(2*len(p)))
	for _, u := range names {
		data = binary.LittleEndian.AppendUint16(data, u)
	}
	return setReparse(h, windows.IO_REPARSE_TAG_MOUNT_POINT, data)
}

// setReparse sets a reparse point of tag, holding data, on the empty folder open at h, with
// FSCTL_SET_REPARSE_POINT, and answers the system's error.
func setReparse(h windows.Handle, tag uint32, data []byte) error {
	buf := binary.LittleEndian.AppendUint32(nil, tag)
	buf = binary.LittleEndian.AppendUint16(buf, uint16(len(data)))
	buf = binary.LittleEndian.AppendUint16(buf, 0) // reserved
	buf = append(buf, data...)
	var got uint32
	return windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &buf[0], uint32(len(buf)), nil, 0, &got, nil)
}

// clearMountPoint takes the junction off the folder open at h, with FSCTL_DELETE_REPARSE_POINT,
// so the folder's own entries are listed again, not its target's.
func clearMountPoint(t *testing.T, h windows.Handle) {
	t.Helper()
	data := binary.LittleEndian.AppendUint32(nil, windows.IO_REPARSE_TAG_MOUNT_POINT)
	data = binary.LittleEndian.AppendUint32(data, 0) // no data, and reserved
	var got uint32
	if err := windows.DeviceIoControl(h, windows.FSCTL_DELETE_REPARSE_POINT, &data[0], uint32(len(data)), nil, 0, &got, nil); err != nil {
		t.Fatal(err)
	}
}

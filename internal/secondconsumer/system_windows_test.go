package secondconsumer_test

import (
	"encoding/binary"
	"fmt"
	"syscall"
)

// On Windows a file or folder go-files makes has the owner-only access list and no owner by uid
// and gid, so nothing has mode bits to check: write's KeepMode and KeepOwner over the watcher's
// own settings, and confine.Mkdir with the attic's mode, keep or make that list, and the rows
// check the list as trust reads it.
const modes = false

// longPathsSetting reads LongPathsEnabled, under HKLM\SYSTEM\CurrentControlSet\Control\FileSystem,
// for the long-path row to log what it ran under. It opens the key to read alone and writes
// nothing; a key or value it cannot read is said so, never a failure.
func longPathsSetting() string {
	const key, value = `SYSTEM\CurrentControlSet\Control\FileSystem`, "LongPathsEnabled"
	k, err := syscall.UTF16PtrFromString(key)
	if err != nil {
		return fmt.Sprintf("%s: %v", value, err)
	}
	v, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return fmt.Sprintf("%s: %v", value, err)
	}
	var h syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, k, 0, syscall.KEY_READ, &h); err != nil {
		return fmt.Sprintf("%s: the key could not be read: %v", value, err)
	}
	defer syscall.RegCloseKey(h)
	var typ uint32
	var buf [4]byte
	n := uint32(len(buf))
	if err := syscall.RegQueryValueEx(h, v, nil, &typ, &buf[0], &n); err != nil {
		return fmt.Sprintf("%s: not set (%v)", value, err)
	}
	if typ != syscall.REG_DWORD || n != uint32(len(buf)) {
		return fmt.Sprintf("%s: of type %d and %d bytes, not a DWORD", value, typ, n)
	}
	return fmt.Sprintf("%s: %d", value, binary.LittleEndian.Uint32(buf[:]))
}

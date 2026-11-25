package confine

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// volumeNameNT is GetFinalPathNameByHandle's VOLUME_NAME_NT, from fileapi.h: the path as the NT
// object manager names it, its device first.
const volumeNameNT = 0x2

// askFolding asks the volume of f, the folder root opened, where it is, its file system and its
// flags, and the folder its own case sensitivity.
func askFolding(f *os.File, root string) (Folding, error) {
	h := windows.Handle(f.Fd())
	var path [windows.MAX_LONG_PATH]uint16
	n, err := windows.GetFinalPathNameByHandle(h, &path[0], uint32(len(path)), volumeNameNT)
	if err != nil {
		return FoldUnknown, &os.PathError{Op: "GetFinalPathNameByHandle", Path: root, Err: err}
	}
	var flags uint32
	var name [windows.MAX_PATH + 1]uint16
	if err := windows.GetVolumeInformationByHandle(h, nil, 0, nil, nil, &flags, &name[0], uint32(len(name))); err != nil {
		return FoldUnknown, &os.PathError{Op: "GetVolumeInformationByHandle", Path: root, Err: err}
	}
	var cs uint32
	var iosb windows.IO_STATUS_BLOCK
	csErr := windows.NtQueryInformationFile(h, &iosb, (*byte)(unsafe.Pointer(&cs)), uint32(unsafe.Sizeof(cs)), windows.FileCaseSensitiveInformation) //nolint:gosec // G103: the pointer is to cs, a FILE_CASE_SENSITIVE_INFORMATION, for the one call
	return windowsFolding(root, windows.UTF16ToString(path[:min(int(n), len(path))]), windows.UTF16ToString(name[:]), flags, cs, csErr)
}

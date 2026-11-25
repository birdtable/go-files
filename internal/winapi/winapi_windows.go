// Package winapi holds the Windows calls write, confine, lock and appdirs share, one copy each,
// and the make of a file or folder with the owner-only access list each of them
// gives what it makes private. It imports the standard library and
// golang.org/x/sys alone.
package winapi

import (
	"errors"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// How is what OpenAt asks of NtCreateFile; Attributes is OBJECT_ATTRIBUTES', as OBJ_CASE_INSENSITIVE.
// security, MakePrivate's alone, is the descriptor a file made is made with.
type How struct {
	Access, Share, Disposition, Options, Attributes uint32
	security                                        *windows.SECURITY_DESCRIPTOR
}

// OpenAt opens name in the folder open at dir as how says, with SYNCHRONIZE, following no reparse
// point. The error is NtCreateFile's NTSTATUS.
func OpenAt(dir windows.Handle, name string, how How) (h windows.Handle, err error) {
	u, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return 0, err
	}
	attrs := windows.OBJECT_ATTRIBUTES{Length: uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})), RootDirectory: dir, ObjectName: u, Attributes: how.Attributes, SecurityDescriptor: how.security}
	err = windows.NtCreateFile(&h, how.Access|windows.SYNCHRONIZE, &attrs, &windows.IO_STATUS_BLOCK{}, nil, 0,
		how.Share, how.Disposition, how.Options|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	return h, err
}

// renameInfo is FILE_RENAME_INFORMATION and FILE_RENAME_INFORMATION_EX, which share one layout.
type renameInfo struct {
	flags          uint32
	rootDirectory  windows.Handle
	fileNameLength uint32
	fileName       [windows.MAX_PATH]uint16
}

const fileRenameInformationEx = 65 // a FILE_INFORMATION_CLASS x/sys does not name

// Rename renames the file open at h to name in the folder open at dir. With replace it goes over a
// file there, by FileRenameInformationEx with POSIX semantics or, where a volume refuses that
// (FAT), ReplaceIfExists, as os.Root does. The error is a Win32 one (Dos).
func Rename(h, dir windows.Handle, name string, replace bool) error {
	n, err := windows.UTF16FromString(name)
	if err != nil {
		return err
	}
	info := renameInfo{rootDirectory: dir}
	if len(n) > len(info.fileName) {
		return windows.ERROR_FILENAME_EXCED_RANGE
	}
	copy(info.fileName[:], n)
	info.fileNameLength = uint32(2 * (len(n) - 1)) //nolint:gosec // G115: n fits fileName, MAX_PATH long, as checked above
	if replace {
		info.flags = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS
		if err := windows.NtSetInformationFile(h, &windows.IO_STATUS_BLOCK{}, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), fileRenameInformationEx); err == nil { //nolint:gosec // G103: the pointer is to info, a FILE_RENAME_INFORMATION_EX, for the one call
			return nil
		}
		info.flags = 1 // ReplaceIfExists
	}
	return Dos(windows.NtSetInformationFile(h, &windows.IO_STATUS_BLOCK{}, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), windows.FileRenameInformation)) //nolint:gosec // G103: the pointer is to info, a FILE_RENAME_INFORMATION, for the one call
}

// Dos is err with an NTSTATUS answered as its Win32 error, which errors.Is matches to fs's errors.
func Dos(err error) error {
	if st, ok := errors.AsType[windows.NTStatus](err); ok {
		return st.Errno()
	}
	return err
}

// Retried is op, tried again for about 600 ms, waiting 5 ms and twice as long up to 320 ms, while
// it is a sharing violation or an access denial, as a file another process holds answers; a denial
// by an access list too reaches the caller that late.
func Retried(op func() error) error {
	err := op()
	for wait := 5 * time.Millisecond; (errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED)) && wait <= 320*time.Millisecond; wait *= 2 {
		<-time.After(wait)
		err = op()
	}
	return err
}

const fileLinkInformation = 11 // FILE_INFORMATION_CLASS; FileRenameInformation's layout, flags false

// Link makes name in the folder open at dir a hard link to the file open at h, over nothing: by
// FileLinkInformation on the handle, as os.Root's link does after its own open by name, so only
// the file h is open at can be linked. The error is a Win32 one (Dos).
func Link(h, dir windows.Handle, name string) error {
	n, err := windows.UTF16FromString(name)
	if err != nil {
		return err
	}
	info := renameInfo{rootDirectory: dir}
	if len(n) > len(info.fileName) {
		return windows.ERROR_FILENAME_EXCED_RANGE
	}
	copy(info.fileName[:], n)
	info.fileNameLength = uint32(2 * (len(n) - 1)) //nolint:gosec // G115: n fits fileName, MAX_PATH long, as checked above

	st := windows.NtSetInformationFile(h, &windows.IO_STATUS_BLOCK{}, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), fileLinkInformation) //nolint:gosec // G103: the pointer is to info, a FILE_LINK_INFORMATION, for the one call
	return Dos(st)
}

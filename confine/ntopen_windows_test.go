package confine

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A file made by FILE_CREATE where a folder is, as LinkFile makes its link, is answered
// STATUS_FILE_IS_A_DIRECTORY by the system, not STATUS_OBJECT_NAME_COLLISION: the status ntOpen
// reads as a taken name, whose Win32 error, ERROR_ACCESS_DENIED, would say otherwise.
// TestLinkFileRefusesANameOutsideTheRootOrThroughALink holds LinkFile's answer, fs.ErrExist.
//
// What it costs when this fails: the system answers another status, and ntOpen's reading of this
// one hides a LinkFile at a folder's name refused as an access denial, not as the taken name it is.
func TestOnWindowsAFileMadeWhereAFolderIsIsAnsweredFileIsADirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	top, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer top.Close()
	u, err := windows.NewNTUnicodeString("folder")
	if err != nil {
		t.Fatal(err)
	}
	attrs := windows.OBJECT_ATTRIBUTES{Length: uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})), RootDirectory: windows.Handle(top.Fd()), ObjectName: u}
	var h windows.Handle
	err = windows.NtCreateFile(&h, windows.FILE_WRITE_DATA|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE, &attrs, &windows.IO_STATUS_BLOCK{}, nil, 0, 0,
		windows.FILE_CREATE, windows.FILE_NON_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	if err == nil {
		_ = windows.CloseHandle(h)
	}
	if !errors.Is(err, windows.STATUS_FILE_IS_A_DIRECTORY) {
		t.Errorf("NtCreateFile of a file at a folder = %#v, want STATUS_FILE_IS_A_DIRECTORY", err)
	}
}

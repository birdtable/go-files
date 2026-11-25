package confine_test

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// foldsFolders is the folders beyond the temp folder that Folds is held to on Windows: an empty
// folder set case-sensitive, where the volume and the account let it be set, and the temp folder
// reached by the administrative share, a network path Folds does not answer, where the runner can
// reach it.
func foldsFolders(t *testing.T) []string {
	t.Helper()
	var dirs []string
	if dir, err := caseSensitive(t); err != nil {
		t.Logf("no case-sensitive folder on this runner: %v", err)
	} else {
		dirs = append(dirs, dir)
	}
	dir := t.TempDir()
	if vol := filepath.VolumeName(dir); len(vol) == 2 {
		share := `\\localhost\` + vol[:1] + `$` + dir[2:]
		if _, err := os.Stat(share); err != nil {
			t.Logf("no administrative share on this runner: %v", err)
		} else {
			dirs = append(dirs, share)
		}
	}
	return dirs
}

// caseSensitive is an empty folder in the temp folder set case-sensitive, or why it cannot be set.
func caseSensitive(t *testing.T) (string, error) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sensitive")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(name, windows.FILE_WRITE_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	flags := uint32(windows.FILE_CS_FLAG_CASE_SENSITIVE_DIR)
	var iosb windows.IO_STATUS_BLOCK
	return dir, windows.NtSetInformationFile(h, &iosb, (*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags)), windows.FileCaseSensitiveInformation) //nolint:gosec // G103: the pointer is to flags, a FILE_CASE_SENSITIVE_INFORMATION, for the one call
}

package treesum_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"

	"github.com/birdtable/go-files/confine"
	"golang.org/x/sys/windows"
)

// What it costs when this fails: a junction, which any account may make with no privilege, is
// walked as a folder, and the digest pins wherever it leads, outside the tree as well.
func TestAJunctionIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outside := t.TempDir()
	plantJunction(t, outside, filepath.Join(dir, "j"))
	refused(t, rootAt(t, dir), "j", confine.ErrOutside)
}

// plantJunction makes link a junction, a mount point reparse point, to the folder target, with
// FSCTL_SET_REPARSE_POINT, as confine's tests do.
func plantJunction(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Mkdir(link, 0o755); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(link)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(name, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	abs, err := filepath.Abs(target)
	if err != nil {
		t.Fatal(err)
	}
	sub := utf16.Encode([]rune(`\??\` + abs))
	shown := utf16.Encode([]rune(abs))
	// The path buffer holds the substitute name and the print name, each ending in a NUL.
	names := make([]uint16, 0, len(sub)+len(shown)+2)
	names = append(append(append(append(names, sub...), 0), shown...), 0)
	data := make([]byte, 0, 16+2*len(names))
	data = binary.LittleEndian.AppendUint32(data, windows.IO_REPARSE_TAG_MOUNT_POINT)
	data = binary.LittleEndian.AppendUint16(data, uint16(8+2*len(names)))
	data = binary.LittleEndian.AppendUint16(data, 0) // reserved
	data = binary.LittleEndian.AppendUint16(data, 0) // the substitute name's offset
	data = binary.LittleEndian.AppendUint16(data, uint16(2*len(sub)))
	data = binary.LittleEndian.AppendUint16(data, uint16(2*(len(sub)+1)))
	data = binary.LittleEndian.AppendUint16(data, uint16(2*len(shown)))
	for _, u := range names {
		data = binary.LittleEndian.AppendUint16(data, u)
	}
	var got uint32
	if err := windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &data[0], uint32(len(data)), nil, 0, &got, nil); err != nil {
		t.Fatal(err)
	}
}

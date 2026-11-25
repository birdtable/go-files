//go:build windows

package appdirs

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/birdtable/go-files/trust"
)

// testDir is a folder right in the top of the system drive, removed when the test ends, with a
// list of its own that inherits nothing and lets in this account, SYSTEM and Administrators alone,
// so the walk passes it as trust's own Windows tests do: t.TempDir is under AppData, which an
// app's capability SID may change on some machines.
func testDir(t *testing.T) string {
	t.Helper()
	me, err := trust.Account()
	if err != nil {
		t.Fatal(err)
	}
	sys, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + me + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if err != nil {
		t.Fatal(err)
	}
	sa := windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(sa))
	top := filepath.VolumeName(sys) + `\`
	for range 10 {
		d := filepath.Join(top, "go-files-appdirs-test-"+rand.Text())
		name, err := windows.UTF16PtrFromString(d)
		if err != nil {
			t.Fatal(err)
		}
		err = windows.CreateDirectory(name, &sa)
		if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			continue
		}
		if err != nil {
			t.Fatalf("a folder in %s for the test: %v", top, err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(d); err != nil {
				t.Errorf("removing the test's folder: %v", err)
			}
		})
		if _, err := trust.Path(d, trust.Folder, trust.Who{NoLinks: true}); err != nil {
			t.Fatalf("the test's own folder %s does not pass, so no case here says anything: %v", d, err)
		}
		return d
	}
	t.Fatalf("no free name for a folder in %s", top)
	return ""
}

// link makes at a junction to the folder target, which takes no privilege, as a symbolic link
// does without Developer Mode.
func link(t *testing.T, at, target string) {
	t.Helper()
	if err := os.Mkdir(at, 0o700); err != nil {
		t.Fatal(err)
	}
	substitute, shown := utf16.Encode([]rune(`\??\`+target)), utf16.Encode([]rune(target))
	names := make([]uint16, 0, len(substitute)+len(shown)+2)
	names = append(append(append(append(names, substitute...), 0), shown...), 0)
	b := binary.LittleEndian.AppendUint32(nil, windows.IO_REPARSE_TAG_MOUNT_POINT)
	for _, v := range []int{8 + 2*len(names), 0, 0, 2 * len(substitute), 2 * (len(substitute) + 1), 2 * len(shown)} {
		b = binary.LittleEndian.AppendUint16(b, uint16(v))
	}
	for _, c := range names {
		b = binary.LittleEndian.AppendUint16(b, c)
	}
	p, err := windows.UTF16PtrFromString(at)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	var n uint32
	err = windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &b[0], uint32(len(b)), nil, 0, &n, nil)
	if cerr := windows.CloseHandle(h); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatalf("make %s a junction to %s: %v", at, target, err)
	}
}

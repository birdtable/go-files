package lock

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unicode/utf16"
)

// junctionToPipes makes a junction in a new temp folder whose substitute name is the pipes' own
// folder, \Device\NamedPipe\, and answers its path, or "" where the system refuses it, logged.
// lock's tests take the standard library alone besides go-files, so the reparse tag and the
// control code are winnt.h's and winioctl.h's numbers.
func junctionToPipes(t *testing.T) string {
	t.Helper()
	const mountPoint, setReparsePoint = 0xA0000003, 0x000900A4 // IO_REPARSE_TAG_MOUNT_POINT, FSCTL_SET_REPARSE_POINT
	j := filepath.Join(t.TempDir(), "pipes")
	if err := os.Mkdir(j, 0o700); err != nil {
		t.Fatal(err)
	}
	s := utf16.Encode([]rune(`\Device\NamedPipe\`))
	names := append(append(append(append(make([]uint16, 0, 2*len(s)+2), s...), 0), s...), 0)
	b := binary.LittleEndian.AppendUint32(nil, mountPoint)
	for _, v := range []int{8 + 2*len(names), 0, 0, 2 * len(s), 2 * (len(s) + 1), 2 * len(s)} {
		b = binary.LittleEndian.AppendUint16(b, uint16(v))
	}
	for _, c := range names {
		b = binary.LittleEndian.AppendUint16(b, c)
	}
	p, err := syscall.UTF16PtrFromString(j)
	if err != nil {
		t.Fatal(err)
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	var n uint32
	err = syscall.DeviceIoControl(h, setReparsePoint, &b[0], uint32(len(b)), nil, 0, &n, nil)
	if cerr := syscall.CloseHandle(h); err == nil {
		err = cerr
	}
	if err != nil {
		t.Logf("native: a junction %s to \\Device\\NamedPipe\\: %v", j, err)
		return ""
	}
	return j
}

// pipesFolders are the folders whose names are a host's pipes: \\.\pipe\, the pipes share by its
// UNC names, its \\?\GLOBALROOT\Device\Mup\ names and two spellings the system may fold to its
// name, and the pipes' own folder by a junction to it.
func pipesFolders(t *testing.T) []string {
	t.Helper()
	folders := []string{`\\.\pipe\`, `\\localhost\IPC$\`, `\\127.0.0.1\pipe\`, `\\?\UNC\localhost\IPC$\`,
		`\\?\GLOBALROOT\Device\Mup\localhost\IPC$\`, `\\?\GLOBALROOT\Device\Mup\127.0.0.1\pipe\`,
		`\\localhost\\IPC$\`, `\\localhost\IPC$.\`}
	if j := junctionToPipes(t); j != "" {
		folders = append(folders, j)
	}
	return folders
}

// pipesRefused wants err, a call's at name in folder, to be a refusal matching fs.ErrInvalid.
func pipesRefused(t *testing.T, call, name, folder string, err error) {
	t.Helper()
	switch {
	case err == nil:
		t.Errorf("%s of %s in %s: no error, and want it refused", call, name, folder)
	case !errors.Is(err, fs.ErrInvalid):
		t.Errorf("%s of %s in %s: %v, and want it refused, matching fs.ErrInvalid", call, name, folder, err)
	}
}

// Each call of lock that takes a path, Take, Wait and Holder, given a lock or a note in a folder
// whose name is a host's pipes, by the pipes share's name in any spelling, by \\.\pipe\, or by a
// junction to the pipes' own folder, refuses it, matching fs.ErrInvalid, before any open of the
// name: the name by its spelling, and a folder a link leads to the pipes by its own handle. Each
// call that takes a root, TakeIn, WaitIn and HolderIn, refuses a root opened on such a folder, as
// confine refuses one, before any open beneath it; a folder the system opens no root on is logged
// as native. lock's tests take no pipe server: its test layer allows the standard library alone,
// and internal/winapi's and confine's tests show the same check leaves a pipe with no client.
//
// What it costs when this fails: a lock path or a root a program is given, or a link on its way,
// leads lock's look and open to a host's pipe, and the pipe's server may act as the program.
func TestEachCallRefusesALockOrANoteInAHostsPipes(t *testing.T) {
	t.Parallel()
	disk := t.TempDir()
	for _, folder := range pipesFolders(t) {
		name := "go-files-test-" + rand.Text()
		path := filepath.Join(folder, name)
		_, err := Take(path, path+".holder")
		pipesRefused(t, "Take", name, folder, err)
		_, err = Take(filepath.Join(disk, name), path)
		pipesRefused(t, "Take, its note", name, folder, err)
		_, err = Wait(t.Context(), path, "")
		pipesRefused(t, "Wait", name, folder, err)
		_, _, err = Holder(path)
		pipesRefused(t, "Holder", name, folder, err)
		root, err := os.OpenRoot(folder)
		if err != nil {
			t.Logf("native: os.OpenRoot of %s: %v", folder, err)
			continue
		}
		t.Cleanup(func() { _ = root.Close() })
		_, err = TakeIn(root, name, name+".holder")
		pipesRefused(t, "TakeIn", name, folder, err)
		_, err = WaitIn(t.Context(), root, name, "")
		pipesRefused(t, "WaitIn", name, folder, err)
		_, _, err = HolderIn(root, name)
		pipesRefused(t, "HolderIn", name, folder, err)
	}
}

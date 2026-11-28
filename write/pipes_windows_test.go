package write

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// pipeListening makes the first instance of a new named pipe whose ConnectNamedPipe never waits,
// and answers its name below \\.\pipe\ and a check that its server has seen no client.
func pipeListening(t *testing.T) (name string, reached func() error) {
	t.Helper()
	name = "go-files-test-" + rand.Text()
	p, err := windows.UTF16PtrFromString(`\\.\pipe\` + name)
	if err != nil {
		t.Fatal(err)
	}
	server, err := windows.CreateNamedPipe(p, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_NOWAIT, 1, 512, 512, 0, nil)
	if err != nil {
		t.Fatalf("make the pipe %s: %v", name, err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(server) })
	return name, func() error {
		if err := windows.ConnectNamedPipe(server, nil); !errors.Is(err, windows.ERROR_PIPE_LISTENING) {
			return fmt.Errorf("the pipe's server saw a client: ConnectNamedPipe answered %w, not ERROR_PIPE_LISTENING", err)
		}
		return nil
	}
}

// pipesJunction makes a junction in a new temp folder whose substitute name is the pipes' own
// folder, \Device\NamedPipe\, and answers its path, or "" where the system refuses it, logged.
func pipesJunction(t *testing.T) string {
	t.Helper()
	j := filepath.Join(t.TempDir(), "pipes")
	if err := os.Mkdir(j, 0o700); err != nil {
		t.Fatal(err)
	}
	s := utf16.Encode([]rune(`\Device\NamedPipe\`))
	names := append(append(append(append(make([]uint16, 0, 2*len(s)+2), s...), 0), s...), 0)
	b := binary.LittleEndian.AppendUint32(nil, windows.IO_REPARSE_TAG_MOUNT_POINT)
	for _, v := range []int{8 + 2*len(names), 0, 0, 2 * len(s), 2 * (len(s) + 1), 2 * len(s)} {
		b = binary.LittleEndian.AppendUint16(b, uint16(v))
	}
	for _, c := range names {
		b = binary.LittleEndian.AppendUint16(b, c)
	}
	p, err := windows.UTF16PtrFromString(j)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	var n uint32
	err = windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &b[0], uint32(len(b)), nil, 0, &n, nil)
	if cerr := windows.CloseHandle(h); err == nil {
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
	if j := pipesJunction(t); j != "" {
		folders = append(folders, j)
	}
	return folders
}

// pipeRefused wants err to refuse the pipe name, matching fs.ErrInvalid, and the pipe to have
// seen no client.
func pipeRefused(t *testing.T, call, name, folder string, err error, reached func() error) {
	t.Helper()
	switch {
	case err == nil:
		t.Errorf("%s of the pipe %s in %s: no error, and want it refused", call, name, folder)
	case !errors.Is(err, fs.ErrInvalid):
		t.Errorf("%s of the pipe %s in %s: %v, and want it refused, matching fs.ErrInvalid", call, name, folder, err)
	}
	if err := reached(); err != nil {
		t.Errorf("%s of the pipe %s in %s: %v", call, name, folder, err)
	}
}

// pathCall is a call of write that takes a path, tried at pipe.
type pathCall struct {
	call string
	try  func(pipe string) error
}

// pathCalls are the calls of write that take a path, File, FileFrom, Keep (at its copy and at its
// source), PutBack (at the file and at the copy) and Swap, each tried at a path, with a file on a
// disk for the side the path is not, and opts given to each.
func pathCalls(t *testing.T, opts ...Option) []pathCall {
	t.Helper()
	disk := t.TempDir()
	file := filepath.Join(disk, "file")
	if err := os.WriteFile(file, []byte("kept\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	holds := sha256.Sum256([]byte("kept\n"))
	return []pathCall{
		{"File", func(pipe string) error { return File(pipe, []byte("x"), opts...) }},
		{"FileFrom", func(pipe string) error { return FileFrom(pipe, strings.NewReader("x"), opts...) }},
		{"Keep, its copy", func(pipe string) error {
			_, err := Keep(file, filepath.Dir(pipe), filepath.Base(pipe), opts...)
			return err
		}},
		{"Keep, its source", func(pipe string) error {
			_, err := Keep(pipe, disk, "copy", opts...)
			return err
		}},
		{"PutBack, the file", func(pipe string) error { return PutBack(file, pipe, holds, opts...) }},
		{"PutBack, the copy", func(pipe string) error { return PutBack(pipe, file, holds, opts...) }},
		{"Swap", func(pipe string) error {
			return Swap(pipe, filepath.Join(filepath.Dir(pipe), "fresh"), filepath.Join(filepath.Dir(pipe), "aside"), opts...)
		}},
	}
}

// Each call of write that takes a path (pathCalls), given a pipe's name in a folder whose name is
// a host's pipes, by the pipes share's name in any spelling, by \\.\pipe\, or by a junction to the
// pipes' own folder, refuses it, matching fs.ErrInvalid, and the pipe sees no client: the name is
// refused before any open, and a folder a link leads to the pipes by its own handle, before any
// open beneath it.
//
// What it costs when this fails: a path a program is given, or a link on its way, leads write's
// open, which passes no quality of service, to a host's pipe, and the pipe's server may act as the
// program.
func TestEachCallByPathRefusesAPipesFolderAndThePipeSeesNoClient(t *testing.T) {
	t.Parallel()
	for _, folder := range pipesFolders(t) {
		for _, c := range pathCalls(t) {
			name, reached := pipeListening(t)
			pipeRefused(t, c.call, name, folder, c.try(filepath.Join(folder, name)), reached)
		}
	}
}

// Each call of write that takes a path (pathCalls), given a name below a pipe by a junction to the
// pipes' own folder, so that the folder it works in is that pipe, refuses it, matching
// fs.ErrInvalid, before Go's open of the folder, os.OpenRoot: the folder is read first by its own
// handle, from an open that asks for identification alone. That open is the pipe's client, and
// writes nothing; what the pipe saw is logged as native.
//
// What it costs when this fails: Go's open, which passes no quality of service, reaches the pipe
// as its client, and the pipe's server may act as the program.
func TestEachCallByPathReadsAFolderThatIsAPipeAtIdentificationBeforeGoOpensIt(t *testing.T) {
	t.Parallel()
	j := pipesJunction(t)
	if j == "" {
		t.Fatal("no junction to the pipes' folder was made, so nothing here is shown")
	}
	var seen []string
	for _, c := range pathCalls(t, goOpenWith(func(dir string) { seen = append(seen, dir) })) {
		name, reached := pipeListening(t)
		err := c.try(filepath.Join(j, name, "x"))
		if !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("%s below the pipe %s by %s: %v, and want it refused, matching fs.ErrInvalid", c.call, name, j, err)
		}
		t.Logf("native: %s below the pipe %s: %v; %v", c.call, name, err, reached())
	}
	for _, dir := range seen {
		if strings.HasPrefix(strings.ToLower(dir), strings.ToLower(j)) {
			t.Errorf("Go's open, os.OpenRoot, opened %s, below the junction to the pipes' folder, before the folder was read by its own handle", dir)
		}
	}
}

// Each call of write that takes a root, FileIn, Append, LogWriter's Write, KeepIn (from and to),
// PutBackIn (from and to) and SyncFolder, given a root opened on a folder whose name is a host's
// pipes, refuses it, matching fs.ErrInvalid, before any open beneath it, and the pipe whose name
// it was given sees no client. A folder the system opens no root on is logged as native.
//
// What it costs when this fails: a root a program is given on a host's pipes has write open a
// name beneath it, with no quality of service, as that host's pipe, and the pipe's server may act
// as the program.
func TestEachCallByRootRefusesARootOnAHostsPipesAndThePipeSeesNoClient(t *testing.T) {
	t.Parallel()
	disk, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = disk.Close() })
	if err := disk.WriteFile("file", []byte("kept\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	holds := sha256.Sum256([]byte("kept\n"))
	for _, folder := range pipesFolders(t) {
		root, err := os.OpenRoot(folder)
		if err != nil {
			t.Logf("native: os.OpenRoot of %s: %v", folder, err)
			continue
		}
		t.Cleanup(func() { _ = root.Close() })
		for _, c := range []struct {
			call string
			try  func(pipe string) error
		}{
			{"FileIn", func(pipe string) error { return FileIn(root, pipe, []byte("x")) }},
			{"Append", func(pipe string) error { return Append(t.Context(), root, pipe, []byte("x\n"), 1<<20) }},
			{"LogWriter's Write", func(pipe string) error {
				_, err := LogWriter(t.Context(), root, pipe, 1<<20).Write([]byte("x\n"))
				return err
			}},
			{"KeepIn, from", func(pipe string) error { return KeepIn(root, pipe, disk, "copy") }},
			{"KeepIn, to", func(pipe string) error { return KeepIn(disk, "file", root, pipe) }},
			{"PutBackIn, from", func(pipe string) error { return PutBackIn(root, pipe, disk, "file", holds) }},
			{"PutBackIn, to", func(pipe string) error { return PutBackIn(disk, "file", root, pipe, holds) }},
			{"SyncFolder", func(pipe string) error { return SyncFolder(root, pipe) }},
		} {
			name, reached := pipeListening(t)
			pipeRefused(t, c.call, name, folder, c.try(name), reached)
		}
	}
}

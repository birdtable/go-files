package winapi

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// TestAPathIsOnAHostsPipesShareOnlyWhereItsShareIsIPCOrPipe: PipesShare's rule. A path is on a
// host's pipes share where its share is IPC$ or pipe, in any case, for any host, in each spelling of
// a UNC path, under \Device\Mup\ (a redirector's own components passed over) and under
// \\?\GLOBALROOT\Device\Mup\ with each device prefix, with empty components passed over and a
// share name's trailing dots and spaces not read; a path on any other share, a share whose name
// only looks like one, IPC$ or pipe below another share, and a path on a disk are on none.
//
// What it costs when this fails: a root or a folder on a host's pipes share passes the pipes check
// of confine, lock, write and this package's makes, and a name beneath it opens that host's pipe
// with no quality of service, so the pipe's server may act as the program; or, the other way, a
// root on a share of files is refused as if it were pipes.
func TestAPathIsOnAHostsPipesShareOnlyWhereItsShareIsIPCOrPipe(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		path string
		want bool
	}{
		{`\Device\Mup\localhost\IPC$`, true},
		{`\Device\Mup\127.0.0.1\pipe\x`, true},
		{`\DEVICE\MUP\Host.Example.COM\ipc$\`, true},
		{`\Device\Mup\;LanmanRedirector\;Z:0000000000012345\h\IPC$\x`, true},
		{`\\?\GLOBALROOT\Device\Mup\localhost\IPC$\`, true},
		{`\\.\GLOBALROOT\Device\Mup\127.0.0.1\pipe\`, true},
		{`\??\globalroot\device\mup\h\Pipe\x`, true},
		{`//?/GLOBALROOT/Device/Mup/h/ipc$`, true},
		{`\\HOST.example\Ipc$`, true},
		{`\\localhost\pipe\`, true},
		{`\\[::1]\IPC$\`, true},
		{`\\?\UNC\h\PIPE\`, true},
		{`\\.\UNC\h\ipc$`, true},
		{`\??\UNC\h\pipe\x`, true},
		{`//h/ipc$/`, true},
		{`\\.\pipe\`, true},
		{`\\localhost\\IPC$\`, true},
		{`\\\localhost\IPC$\`, true},
		{`\\localhost\IPC$.\`, true},
		{`\\localhost\pipe. .\x`, true},
		{`\\?\UNC\h\\ipc$ `, true},
		{`\Device\Mup\localhost\C$\x`, false},
		{`\Device\Mup\;LanmanRedirector\;Z:0000000000012345\h\data`, false},
		{`\\?\GLOBALROOT\Device\Mup\localhost\C$\x`, false},
		{`\\?\GLOBALROOT\Device\HarddiskVolume3\pipe`, false},
		{`\\h\pipes\`, false},
		{`\\h\ipc\`, false},
		{`\\h\.pipe`, false},
		{`\\h\share\IPC$`, false},
		{`\\h\share\pipe\x`, false},
		{`\\h`, false},
		{`\\h\\`, false},
		{`\\?\C:\IPC$`, false},
		{`\\.\C:\pipe`, false},
		{`C:\pipe\x`, false},
		{`\Device\HarddiskVolume3\pipe`, false},
		{``, false},
	} {
		if got := PipesShare(c.path); got != c.want {
			t.Errorf("PipesShare(%q): %t, and want %t", c.path, got, c.want)
		}
	}
}

// listeningPipe makes the first instance of a new named pipe whose ConnectNamedPipe never waits,
// and answers its name below \\.\pipe\ and a check that its server has seen no client.
func listeningPipe(t *testing.T) (name string, reached func() error) {
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

// pipesFolders are the folders whose names are a host's pipes: the pipes' own folder by a junction
// planted to \Device\NamedPipe\, \\.\pipe\, and the pipes share by its UNC names, its
// \\?\GLOBALROOT\Device\Mup\ names and two spellings the system may fold to its name. A junction
// the system refuses is logged and left out.
func pipesFolders(t *testing.T) []string {
	t.Helper()
	folders := []string{`\\.\pipe\`, `\\localhost\IPC$\`, `\\127.0.0.1\pipe\`, `\\?\UNC\localhost\IPC$\`,
		`\\?\GLOBALROOT\Device\Mup\localhost\IPC$\`, `\\?\GLOBALROOT\Device\Mup\127.0.0.1\pipe\`,
		`\\localhost\\IPC$\`, `\\localhost\IPC$.\`}
	j := filepath.Join(t.TempDir(), "pipes")
	if err := plantJunction(j, `\Device\NamedPipe\`); err != nil {
		t.Logf("native: a junction %s to \\Device\\NamedPipe\\: %v", j, err)
		return folders
	}
	return append(folders, j)
}

// A folder whose name is a host's pipes, by the pipes share's name in any spelling or by a junction
// to the pipes' own folder, is refused by its own handle where it opens: OnPipes refuses the folder
// OpenFolder opened, and RootOnPipes the root os.OpenRoot opened, each matching fs.ErrInvalid, and
// each folder's final path and file type are logged. MakeFolder and MakeFile, given OpenFolder or
// the root's Open, refuse to make a pipe's name in it, and the pipe sees no client. A folder the
// system does not open is logged as its own answer, and the make in it must still fail and reach
// no pipe. A folder on a disk passes both checks, the control.
//
// What it costs when this fails: a make by path, appdirs' temp folder or a lock file, in a folder
// that a link leads to a host's pipes opens a pipe there with no quality of service, or a root
// on the pipes is not refused, and the pipe's server may act as the program.
func TestAFolderOnAHostsPipesIsRefusedByItsOwnHandleAndNoMakeInItReachesAPipe(t *testing.T) {
	t.Parallel()
	disk := t.TempDir()
	d, err := OpenFolder(disk)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := OnPipes(d, "x"); err != nil {
		t.Errorf("OnPipes of the folder %s on a disk: %v, and want it to pass", disk, err)
	}
	root, err := os.OpenRoot(disk)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	if err := RootOnPipes(root, "x"); err != nil {
		t.Errorf("RootOnPipes of the root %s on a disk: %v, and want it to pass", disk, err)
	}
	listless := Listless{Err: errors.New("no access lists")}
	for _, folder := range pipesFolders(t) {
		opened := folderOnPipes(t, folder)
		for _, m := range []struct {
			what string
			make func(open Open, path string) error
		}{
			{"MakeFolder", func(open Open, path string) error { return MakeFolder(open, path, listless) }},
			{"MakeFile", func(open Open, path string) error {
				f, err := MakeFile(open, path, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE, windows.FILE_SHARE_READ, listless)
				if err == nil {
					err = errors.Join(errors.New("made"), f.Close())
				}
				return err
			}},
		} {
			name, reached := listeningPipe(t)
			err := m.make(OpenFolder, filepath.Join(folder, name))
			switch {
			case err == nil:
				t.Errorf("%s of the pipe %s in %s: no error, and want it refused", m.what, name, folder)
			case opened && !errors.Is(err, fs.ErrInvalid):
				t.Errorf("%s of the pipe %s in %s: %v, and want the folder refused, matching fs.ErrInvalid", m.what, name, folder, err)
			case !opened:
				t.Logf("native: %s of the pipe %s in %s, which does not open: %v", m.what, name, folder, err)
			}
			if err := reached(); err != nil {
				t.Errorf("%s of the pipe %s in %s: %v", m.what, name, folder, err)
			}
		}
		rootOnPipesRefused(t, folder)
	}
}

// folderOnPipes opens folder by OpenFolder, logs its final path and file type, wants OnPipes to
// refuse it, matching fs.ErrInvalid, and answers whether it opened.
func folderOnPipes(t *testing.T, folder string) bool {
	t.Helper()
	f, err := OpenFolder(folder)
	if err != nil {
		t.Logf("native: OpenFolder of %s: %v", folder, err)
		return false
	}
	defer func() { _ = f.Close() }()
	h := windows.Handle(f.Fd())
	var buf [windows.MAX_LONG_PATH]uint16
	n, perr := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), volumeNameNT)
	ft, ferr := windows.GetFileType(h)
	t.Logf("native: %s opened: its final path %q, %v; its file type %d, %v", folder, windows.UTF16ToString(buf[:min(int(n), len(buf))]), perr, ft, ferr)
	if err := OnPipes(f, "x"); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("OnPipes of %s: %v, and want it refused, matching fs.ErrInvalid", folder, err)
	}
	return true
}

// rootOnPipesRefused opens folder by os.OpenRoot and wants RootOnPipes to refuse it, matching
// fs.ErrInvalid, and MakeFile by the root's Open to make no pipe's name in it, the pipe seeing no
// client. A root that does not open is logged as native.
func rootOnPipesRefused(t *testing.T, folder string) {
	t.Helper()
	root, err := os.OpenRoot(folder)
	if err != nil {
		t.Logf("native: os.OpenRoot of %s: %v", folder, err)
		return
	}
	defer func() { _ = root.Close() }()
	if err := RootOnPipes(root, "x"); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("RootOnPipes of the root %s: %v, and want it refused, matching fs.ErrInvalid", folder, err)
	}
	name, reached := listeningPipe(t)
	f, err := MakeFile(root.Open, name, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE, windows.FILE_SHARE_READ, Listless{Err: errors.New("no access lists")})
	switch {
	case err == nil:
		_ = f.Close()
		t.Errorf("MakeFile by the root %s's Open of the pipe %s: no error, and want it refused", folder, name)
	case !errors.Is(err, fs.ErrInvalid):
		t.Errorf("MakeFile by the root %s's Open of the pipe %s: %v, and want the folder refused, matching fs.ErrInvalid", folder, name, err)
	}
	if err := reached(); err != nil {
		t.Errorf("MakeFile by the root %s's Open of the pipe %s: %v", folder, name, err)
	}
}

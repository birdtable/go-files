package winapi

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// pipeServer makes the first instance of a new named pipe, in byte mode, its reads waiting, and
// answers its \\.\pipe\ name, its name below \\.\pipe\, and the server's handle, closed when t ends.
func pipeServer(t *testing.T) (path, name string, server windows.Handle) {
	t.Helper()
	name = "go-files-test-" + rand.Text()
	path = `\\.\pipe\` + name
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	server, err = windows.CreateNamedPipe(p, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT, 1, 512, 512, 0, nil)
	if err != nil {
		t.Fatalf("make the pipe %s: %v", path, err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(server) })
	return path, name, server
}

// clientUser is the name of the account at the client end of server, as GetNamedPipeHandleState
// reads it, which it can do only where the client let the server impersonate it.
func clientUser(server windows.Handle) (string, error) {
	buf := make([]uint16, 512)
	err := windows.GetNamedPipeHandleState(server, nil, nil, nil, nil, &buf[0], uint32(len(buf)))
	return windows.UTF16ToString(buf), err
}

// An open of a named pipe gives its server the client's token at the level the open asks for, and
// with no quality of service asked, as Go's own opens and os.Stat's ask none, at impersonation: a
// server holding SeImpersonatePrivilege may act as the client. Each open of go-files asks for
// identification, at which the server learns who the client is and may act as it in nothing:
// winapi.CreateFile, OpenPath and OpenAt. Two controls show the server can see impersonation where
// it is given: a windows.CreateFile with no quality of service, and the open os.Stat makes, for the
// attributes alone with its reparse point not followed. Each client end is shown connected before
// the server asks, so a refusal is the level's, not a client gone.
//
// What it costs when this fails: a path that leads to a pipe, by a link or a junction a less-trusted
// account planted, lets that pipe's server act as the program that opened it, with every right the
// program's account holds, where the hardening says it may only learn who it is.
func TestAPipeServerMayActAsTheClientOfNoOpenOfGoFiles(t *testing.T) {
	t.Parallel()
	rw := uint32(windows.GENERIC_READ | windows.GENERIC_WRITE)
	byPath := func(open func(*uint16) (windows.Handle, error)) func(*testing.T, string, string) windows.Handle {
		return func(t *testing.T, path, _ string) windows.Handle {
			t.Helper()
			p, err := windows.UTF16PtrFromString(path)
			if err != nil {
				t.Fatal(err)
			}
			h, err := open(p)
			if err != nil {
				t.Fatalf("open %s: %v", path, err)
			}
			return h
		}
	}
	openPath := func(follow bool) func(*testing.T, string, string) windows.Handle {
		return func(t *testing.T, path, _ string) windows.Handle {
			t.Helper()
			f, err := OpenPath(path, follow)
			if err != nil {
				t.Fatalf("OpenPath %s: %v", path, err)
			}
			// The case closes a handle of its own: a second handle of the same open, so the client end
			// stays connected once f is closed.
			var h windows.Handle
			err = windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(f.Fd()), windows.CurrentProcess(), &h, 0, false, windows.DUPLICATE_SAME_ACCESS)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				t.Fatal(err)
			}
			return h
		}
	}
	for _, c := range []struct {
		name   string
		open   func(t *testing.T, path, name string) windows.Handle
		writes bool // whether the client writes a byte, which the server reads before it asks
		acts   bool // whether the server may act as the client
	}{
		{"windows.CreateFile with no quality of service, the control", byPath(func(p *uint16) (windows.Handle, error) {
			return windows.CreateFile(p, rw, 0, nil, windows.OPEN_EXISTING, 0, 0)
		}), true, true},
		{"os.Stat's open for the attributes alone, the control", byPath(func(p *uint16) (windows.Handle, error) {
			return windows.CreateFile(p, 0, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		}), false, true},
		{"CreateFile", byPath(func(p *uint16) (windows.Handle, error) {
			return CreateFile(p, rw, 0, windows.OPEN_EXISTING, 0)
		}), true, false},
		{"CreateFile for the attributes alone", byPath(func(p *uint16) (windows.Handle, error) {
			return CreateFile(p, windows.FILE_READ_ATTRIBUTES, 0, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT)
		}), false, false},
		{"OpenPath", openPath(true), false, false},
		{"OpenPath with no link followed", openPath(false), false, false},
		{"OpenAt", func(t *testing.T, _, name string) windows.Handle {
			t.Helper()
			root, err := windows.UTF16PtrFromString(`\\.\pipe\`)
			if err != nil {
				t.Fatal(err)
			}
			dir, err := windows.CreateFile(root, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
			if err != nil {
				t.Fatalf("open the pipes' folder: %v", err)
			}
			defer windows.CloseHandle(dir)
			h, err := OpenAt(dir, name, How{Access: rw, Share: windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE, Disposition: windows.FILE_OPEN, Options: windows.FILE_NON_DIRECTORY_FILE})
			if err != nil {
				t.Fatalf("OpenAt %s: %v", name, err)
			}
			return h
		}, true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			path, name, server := pipeServer(t)
			client := c.open(t, path, name)
			defer windows.CloseHandle(client)
			if err := windows.ConnectNamedPipe(server, nil); !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
				t.Fatalf("the client end is not connected: ConnectNamedPipe answered %v, not ERROR_PIPE_CONNECTED", err)
			}
			if c.writes {
				var n uint32
				if err := windows.WriteFile(client, []byte{1}, &n, nil); err != nil {
					t.Fatalf("the client's write: %v", err)
				}
				if err := windows.ReadFile(server, make([]byte, 1), &n, nil); err != nil || n != 1 {
					t.Fatalf("the server's read: %d bytes, %v", n, err)
				}
			}
			user, err := clientUser(server)
			switch {
			case c.acts && (err != nil || user == ""):
				t.Fatalf("the control: the server could not impersonate a client that asked for no level (%q, %v), so this test cannot see the level", user, err)
			case !c.acts && err == nil:
				t.Fatalf("the server impersonated the client and read its account, %q: the open asked for more than identification", user)
			case !c.acts:
				t.Logf("the server cannot impersonate the client: %v", err)
			}
		})
	}
}

// plantJunction makes link a new folder and a junction whose substitute name is sub, as written,
// and answers the system's error.
func plantJunction(link, sub string) error {
	if err := os.Mkdir(link, 0o700); err != nil {
		return err
	}
	s := utf16.Encode([]rune(sub))
	names := append(append(append(append(make([]uint16, 0, 2*len(s)+2), s...), 0), s...), 0)
	b := binary.LittleEndian.AppendUint32(nil, windows.IO_REPARSE_TAG_MOUNT_POINT)
	for _, v := range []int{8 + 2*len(names), 0, 0, 2 * len(s), 2 * (len(s) + 1), 2 * len(s)} {
		b = binary.LittleEndian.AppendUint16(b, uint16(v))
	}
	for _, c := range names {
		b = binary.LittleEndian.AppendUint16(b, c)
	}
	p, err := windows.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	var n uint32
	err = windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &b[0], uint32(len(b)), nil, 0, &n, nil)
	if cerr := windows.CloseHandle(h); err == nil {
		err = cerr
	}
	return err
}

// A junction any account may plant can name a pipe, \Device\NamedPipe\<name> or \??\pipe\<name>,
// or the pipes' own folder, with the pipe's name below it in the path. Where the system follows
// one to the pipe, as the open os.Stat makes when it follows a name does (the control, run first
// for each), OpenPath, which Stat, Lstat and the opens of confine and appdirs that follow a path
// are, reaches it at identification alone: the pipe's server learns who the caller is, and may act
// as it in nothing. Where the system follows none, the control says so and nothing else is asked.
//
// What it costs when this fails: a junction a less-trusted account planted in a folder the program
// walks gives that account's pipe server the program's token to act with.
func TestAJunctionToAPipeLetsItsServerActAsNoOpenPath(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		sub  func(pipe string) string
		path func(junction, pipe string) string
	}{
		{`\Device\NamedPipe\<name>`, func(n string) string { return `\Device\NamedPipe\` + n }, func(j, _ string) string { return j }},
		{`\??\pipe\<name>`, func(n string) string { return `\??\pipe\` + n }, func(j, _ string) string { return j }},
		{`\Device\NamedPipe\, the name below it`, func(string) string { return `\Device\NamedPipe\` }, func(j, n string) string { return filepath.Join(j, n) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			reach := func(t *testing.T, at string, open func(path string) (windows.Handle, error)) (windows.Handle, windows.Handle, bool) {
				t.Helper()
				_, name, server := pipeServer(t)
				j := filepath.Join(dir, at)
				if err := plantJunction(j, c.sub(name)); err != nil {
					t.Logf("the system refuses the junction %s: %v", c.name, err)
					return 0, 0, false
				}
				h, err := open(c.path(j, name))
				if err != nil {
					t.Logf("the open through the junction %s: %v", c.name, err)
					return 0, 0, false
				}
				t.Cleanup(func() { _ = windows.CloseHandle(h) })
				if err := windows.ConnectNamedPipe(server, nil); !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
					t.Logf("the open through the junction %s reached no pipe: ConnectNamedPipe answered %v", c.name, err)
					return 0, 0, false
				}
				return server, h, true
			}
			server, _, ok := reach(t, "control", func(path string) (windows.Handle, error) {
				p, err := windows.UTF16PtrFromString(path)
				if err != nil {
					return 0, err
				}
				return windows.CreateFile(p, 0, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
			})
			if !ok {
				t.Logf("native: the system follows no junction %s to a pipe, so no open through one reaches it", c.name)
				return
			}
			if user, err := clientUser(server); err != nil || user == "" {
				t.Fatalf("the control: the server could not impersonate a client that asked for no level (%q, %v), so this test cannot see the level", user, err)
			}
			t.Logf("native: os.Stat's open follows the junction %s to the pipe, and its server may act as the caller", c.name)
			server, _, ok = reach(t, "openpath", func(path string) (windows.Handle, error) {
				f, err := OpenPath(path, true)
				if err != nil {
					return 0, err
				}
				var h windows.Handle
				err = windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(f.Fd()), windows.CurrentProcess(), &h, 0, false, windows.DUPLICATE_SAME_ACCESS)
				if cerr := f.Close(); err == nil {
					err = cerr
				}
				return h, err
			})
			if !ok {
				t.Fatalf("OpenPath reached no pipe through the junction %s where the control's open did, so this test cannot see its level", c.name)
			}
			if user, err := clientUser(server); err == nil {
				t.Fatalf("OpenPath through the junction %s let the pipe's server impersonate the caller and read its account, %q", c.name, user)
			}
		})
	}
}

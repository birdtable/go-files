package winapi

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unicode/utf16"
	"unsafe"

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

// clientLevel is the level at which server's thread takes on the token of the client at its end:
// the thread token's TokenImpersonationLevel after ImpersonateNamedPipeClient, opened as the process,
// as a token at identification opens to nothing else, and then reverted. The system answers
// ERROR_CANNOT_IMPERSONATE until the server has read data the client wrote. A thread that cannot
// revert stays locked, so the scheduler ends it rather than hand it on.
//
// ImpersonateNamedPipeClient is advapi32's, which x/sys does not wrap, and DeviceIoControl cannot
// stand in for it, its control answering ERROR_INVALID_FUNCTION: the one proc go-files acquires,
// on its one line here, which the lint excepts by file and by line.
func clientLevel(server windows.Handle) (level uint32, err error) {
	impersonate := windows.NewLazySystemDLL("advapi32.dll").NewProc("ImpersonateNamedPipeClient")
	runtime.LockOSThread()
	if r, _, err := impersonate.Call(uintptr(server)); r == 0 {
		runtime.UnlockOSThread()
		return 0, err
	}
	var n uint32
	var tok windows.Token
	err = windows.OpenThreadToken(windows.CurrentThread(), windows.TOKEN_QUERY, true, &tok)
	if err == nil {
		err = windows.GetTokenInformation(tok, windows.TokenImpersonationLevel, (*byte)(unsafe.Pointer(&level)), uint32(unsafe.Sizeof(level)), &n) //nolint:gosec // G103: the pointer is to level, a SECURITY_IMPERSONATION_LEVEL, for the one call
		err = errors.Join(err, tok.Close())
	}
	if rerr := windows.RevertToSelf(); rerr != nil {
		return 0, errors.Join(err, fmt.Errorf("the server's thread could not revert, and stays locked: %w", rerr))
	}
	runtime.UnlockOSThread()
	return level, err
}

// levelAfterAByte has client write a byte, server read it, and answers clientLevel.
func levelAfterAByte(t *testing.T, client, server windows.Handle) (uint32, error) {
	t.Helper()
	var n uint32
	if err := windows.WriteFile(client, []byte{1}, &n, nil); err != nil {
		t.Fatalf("the client's write: %v", err)
	}
	if err := windows.ReadFile(server, make([]byte, 1), &n, nil); err != nil || n != 1 {
		t.Fatalf("the server's read: %d bytes, %v", n, err)
	}
	return clientLevel(server)
}

// levels names a SECURITY_IMPERSONATION_LEVEL for a message.
func levels(level uint32) string {
	switch level {
	case windows.SecurityAnonymous:
		return "SecurityAnonymous"
	case windows.SecurityIdentification:
		return "SecurityIdentification"
	case windows.SecurityImpersonation:
		return "SecurityImpersonation"
	case windows.SecurityDelegation:
		return "SecurityDelegation"
	}
	return fmt.Sprint("level ", level)
}

// An open of a named pipe gives its server the client's token at the level the open asks for, and
// with no quality of service asked, as Go's own opens and os.Stat's ask none, at impersonation: a
// server holding SeImpersonatePrivilege may act as the client. Each open of go-files asks for
// identification, at which the server learns who the client is and may act as it in nothing:
// winapi.CreateFile, OpenPath and OpenAt. Each client that may write writes a byte the server reads,
// and the server then reads the level of the token it takes on: SecurityIdentification for
// winapi.CreateFile and OpenAt, and SecurityImpersonation for the control, a windows.CreateFile with
// no quality of service, so the test is seen to tell the two apart. An open for the attributes
// alone, as OpenPath's and os.Stat's are, can write nothing, and the system lets a server take on
// no token from a client that sent it nothing: those are shown refused, ERROR_CANNOT_IMPERSONATE,
// whatever level they asked, their level being winapi.CreateFile's, read above. Each client end is
// shown connected before the server asks, so a refusal is the level's, not a client gone.
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
		writes bool   // whether the client writes a byte, which the server reads before it asks
		want   uint32 // the level a client that writes gives the server
	}{
		{"windows.CreateFile with no quality of service, the control", byPath(func(p *uint16) (windows.Handle, error) {
			return windows.CreateFile(p, rw, 0, nil, windows.OPEN_EXISTING, 0, 0)
		}), true, windows.SecurityImpersonation},
		{"CreateFile", byPath(func(p *uint16) (windows.Handle, error) {
			return CreateFile(p, rw, 0, windows.OPEN_EXISTING, 0)
		}), true, windows.SecurityIdentification},
		{"CreateFile for the attributes alone", byPath(func(p *uint16) (windows.Handle, error) {
			return CreateFile(p, windows.FILE_READ_ATTRIBUTES, 0, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT)
		}), false, 0},
		{"OpenPath", openPath(true), false, 0},
		{"OpenPath with no link followed", openPath(false), false, 0},
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
		}, true, windows.SecurityIdentification},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			path, name, server := pipeServer(t)
			client := c.open(t, path, name)
			defer windows.CloseHandle(client)
			if err := windows.ConnectNamedPipe(server, nil); !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
				t.Fatalf("the client end is not connected: ConnectNamedPipe answered %v, not ERROR_PIPE_CONNECTED", err)
			}
			if !c.writes {
				level, err := clientLevel(server)
				if !errors.Is(err, windows.ERROR_CANNOT_IMPERSONATE) {
					t.Fatalf("the server took on the token of a client that wrote nothing: %s, %v, and want ERROR_CANNOT_IMPERSONATE", levels(level), err)
				}
				t.Logf("native: the server takes on no token from a client that wrote nothing: %v", err)
				return
			}
			level, err := levelAfterAByte(t, client, server)
			switch {
			case err != nil && c.want == windows.SecurityImpersonation:
				t.Fatalf("the control: the server took on no token of a client that asked for no level (%v), so this test cannot see the level", err)
			case err != nil:
				t.Fatalf("the server took on no token of the client (%v), so this test cannot see the level it gives", err)
			case level != c.want:
				t.Fatalf("the server took on the client's token at %s, and want %s", levels(level), levels(c.want))
			}
			t.Logf("native: the server took on the client's token at %s", levels(level))
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
// one to the pipe, as an open with no quality of service does (the control, run first for each,
// whose server takes on the caller's token at SecurityImpersonation once it reads a byte), an open
// by winapi.CreateFile through it gives the server SecurityIdentification alone, and OpenPath, which
// Stat, Lstat and the opens of confine and appdirs that follow a path are, reaches it for the
// attributes alone, writing nothing, so its server takes on no token at all. Where the system
// follows none, the control says so and nothing else is asked.
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
			rw := uint32(windows.GENERIC_READ | windows.GENERIC_WRITE)
			server, client, ok := reach(t, "control", func(path string) (windows.Handle, error) {
				p, err := windows.UTF16PtrFromString(path)
				if err != nil {
					return 0, err
				}
				return windows.CreateFile(p, rw, 0, nil, windows.OPEN_EXISTING, 0, 0)
			})
			if !ok {
				t.Logf("native: the system follows no junction %s to a pipe, so no open through one reaches it", c.name)
				return
			}
			if level, err := levelAfterAByte(t, client, server); err != nil || level != windows.SecurityImpersonation {
				t.Fatalf("the control: the server took on the token of a client that asked for no level at %s, %v, and want SecurityImpersonation, so this test cannot see the level", levels(level), err)
			}
			t.Logf("native: an open with no quality of service follows the junction %s to the pipe, and its server may act as the caller", c.name)
			server, client, ok = reach(t, "createfile", func(path string) (windows.Handle, error) {
				p, err := windows.UTF16PtrFromString(path)
				if err != nil {
					return 0, err
				}
				return CreateFile(p, rw, 0, windows.OPEN_EXISTING, 0)
			})
			if !ok {
				t.Fatalf("CreateFile reached no pipe through the junction %s where the control's open did, so this test cannot see its level", c.name)
			}
			if level, err := levelAfterAByte(t, client, server); err != nil || level != windows.SecurityIdentification {
				t.Fatalf("CreateFile through the junction %s gave the pipe's server the caller's token at %s, %v, and want SecurityIdentification", c.name, levels(level), err)
			}
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
			if level, err := clientLevel(server); !errors.Is(err, windows.ERROR_CANNOT_IMPERSONATE) {
				t.Fatalf("OpenPath through the junction %s gave the pipe's server the caller's token at %s, %v, and want ERROR_CANNOT_IMPERSONATE, as it writes nothing", c.name, levels(level), err)
			}
		})
	}
}

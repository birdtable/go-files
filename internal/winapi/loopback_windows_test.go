package winapi

import (
	"crypto/rand"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// loopbackPipe makes the first instance of a new named pipe, as pipeServer does, with an access
// list that grants this account's SID every right, so an open over the loopback share, made by a
// network logon of this account, may read and write it: in that logon the administrators' group,
// the owner of an elevated process's default list, is for denying alone. It answers the pipe's name
// below \\.\pipe\ and the server's handle, closed when t ends.
func loopbackPipe(t *testing.T) (name string, server windows.Handle) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	name = "go-files-test-" + rand.Text()
	p, err := windows.UTF16PtrFromString(`\\.\pipe\` + name)
	if err != nil {
		t.Fatal(err)
	}
	server, err = windows.CreateNamedPipe(p, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT, 1, 512, 512, 0, sa)
	if err != nil {
		t.Fatalf("make the pipe %s: %v", name, err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(server) })
	return name, server
}

// connected reports whether a client holds server's one instance, asked without waiting: the
// server is set not to wait, so ConnectNamedPipe answers ERROR_PIPE_CONNECTED for a client and
// ERROR_PIPE_LISTENING for none, and is then set back to wait for its reads.
func connected(t *testing.T, server windows.Handle) bool {
	t.Helper()
	set := func(mode uint32) {
		if err := windows.SetNamedPipeHandleState(server, &mode, nil, nil); err != nil {
			t.Fatalf("set the server's wait mode: %v", err)
		}
	}
	set(windows.PIPE_READMODE_BYTE | windows.PIPE_NOWAIT)
	defer set(windows.PIPE_READMODE_BYTE | windows.PIPE_WAIT)
	err := windows.ConnectNamedPipe(server, nil)
	switch {
	case errors.Is(err, windows.ERROR_PIPE_CONNECTED):
		return true
	case errors.Is(err, windows.ERROR_PIPE_LISTENING):
		return false
	}
	t.Fatalf("ConnectNamedPipe answered %v, neither connected nor listening", err)
	return false
}

// A symbolic link can name a host's pipe over the loopback share, \\localhost\pipe\<name>, which the
// pipes check reads from no name on the caller's path, so the folder open of write and lock,
// OpenFolder, is asked to follow it before OnPipes reads a handle. The system's server for the
// share opens the pipe for the caller, and a remote pipe's open ignores the level flags its client
// passes, so the level the pipe's server gets is measured here for each open: the control, a
// windows.CreateFile with no quality of service; CreateFile, which asks for identification; and
// OpenFolder. Each is made at the share's name itself and through a link of each kind, a file's
// and a folder's, to it. An open that reaches the pipe and may write writes a byte the server reads
// first; an open that is refused is shown to leave the pipe with no client. Each answer is logged as
// native. A control must reach the pipe, by one name or another, for the test to see a level. A
// link is made by CreateSymbolicLink itself, as os.Symlink would open its target, the pipe, to
// choose a link's kind; making one needs the right to make links, which CI holds as an
// administrator.
//
// What it costs when this fails: a link a less-trusted account planted on a path the program walks
// lets that account's pipe server, over the loopback share, act as the program.
func TestALinkToTheLoopbackPipesShareGivesItsServerIdentificationAlone(t *testing.T) {
	t.Parallel()
	var reached atomic.Int32 // the controls that reached the pipe
	t.Cleanup(func() {
		if reached.Load() == 0 {
			t.Errorf("no control reached the pipe over the loopback share, by its name or a link, so this test sees no level")
		}
	})
	rw := uint32(windows.GENERIC_READ | windows.GENERIC_WRITE)
	byPath := func(open func(p *uint16) (windows.Handle, error)) func(string) (windows.Handle, error) {
		return func(path string) (windows.Handle, error) {
			p, err := windows.UTF16PtrFromString(path)
			if err != nil {
				return 0, err
			}
			return open(p)
		}
	}
	opens := []struct {
		name    string
		open    func(path string) (windows.Handle, error)
		control bool   // whether it is the control, which counts as reaching the pipe
		writes  bool   // whether the client writes a byte, which the server reads before it asks
		want    uint32 // the level a client that writes should give the server
	}{
		{"windows.CreateFile with no quality of service, the control", byPath(func(p *uint16) (windows.Handle, error) {
			return windows.CreateFile(p, rw, 0, nil, windows.OPEN_EXISTING, 0, 0)
		}), true, true, windows.SecurityImpersonation},
		{"CreateFile", byPath(func(p *uint16) (windows.Handle, error) {
			return CreateFile(p, rw, 0, windows.OPEN_EXISTING, 0)
		}), false, true, windows.SecurityIdentification},
		{"OpenFolder", func(path string) (windows.Handle, error) {
			f, err := OpenFolder(path)
			if err != nil {
				return 0, err
			}
			var h windows.Handle
			err = windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(f.Fd()), windows.CurrentProcess(), &h, 0, false, windows.DUPLICATE_SAME_ACCESS)
			return h, errors.Join(err, f.Close())
		}, false, false, 0},
	}
	ways := []struct {
		name string
		link bool   // whether the open is through a link to the share's name
		kind uint32 // the link's kind
	}{
		{"the share's name", false, 0},
		{"a file's link", true, 0},
		{"a folder's link", true, windows.SYMBOLIC_LINK_FLAG_DIRECTORY},
	}
	for _, w := range ways {
		for _, c := range opens {
			t.Run(c.name+" by "+w.name, func(t *testing.T) {
				t.Parallel()
				name, server := loopbackPipe(t)
				path := `\\localhost\pipe\` + name
				if w.link {
					link := filepath.Join(t.TempDir(), "link")
					l, err := windows.UTF16PtrFromString(link)
					if err != nil {
						t.Fatal(err)
					}
					target, err := windows.UTF16PtrFromString(path)
					if err != nil {
						t.Fatal(err)
					}
					if err := windows.CreateSymbolicLink(l, target, w.kind); err != nil {
						t.Fatalf("make the link to the loopback pipes share (CI holds the right as an administrator): %v", err)
					}
					path = link
				}
				client, err := c.open(path)
				if err != nil {
					has := connected(t, server)
					t.Logf("native: the open is refused (%v), and the pipe has a client: %t", err, has)
					if has {
						t.Errorf("a refused open left the pipe a client")
					}
					return
				}
				defer windows.CloseHandle(client)
				if !connected(t, server) {
					t.Fatalf("the open reached no pipe, and was not refused")
				}
				if c.control {
					reached.Add(1)
				}
				if !c.writes {
					level, err := clientLevel(server)
					t.Logf("native: the server of a client that wrote nothing took on its token at %s, %v", levels(level), err)
					if !errors.Is(err, windows.ERROR_CANNOT_IMPERSONATE) {
						t.Errorf("the server took on the token of a client that wrote nothing: %s, %v, and want ERROR_CANNOT_IMPERSONATE", levels(level), err)
					}
					return
				}
				level, err := levelAfterAByte(t, client, server)
				t.Logf("native: the server took on the client's token at %s, %v", levels(level), err)
				if err != nil || level != c.want {
					t.Errorf("the server took on the client's token at %s, %v, and want %s", levels(level), err, levels(c.want))
				}
			})
		}
	}
}

package winapi

import (
	"errors"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// A symbolic link can name a host's pipe over the loopback share, \\localhost\pipe\<name>, which the
// pipes check reads from no name on the caller's path, so the folder open of write and lock,
// OpenFolder, follows it before OnPipes reads the handle. The system's server for the share opens
// the pipe for the caller, and a remote pipe's open ignores the level flags its client passes, so
// the level the pipe's server gets is measured here, for each open, through such a link: the
// control, a windows.CreateFile with no quality of service; CreateFile, which asks for
// identification; and OpenFolder, which writes nothing. A client that may write writes a byte the
// server reads first. Each level is logged as native. The link is made by CreateSymbolicLink
// itself, as os.Symlink would read its target, the pipe, to choose a link's kind; making it needs
// the right to make links, which CI holds as an administrator, and a system that follows no link to
// a remote name, or serves no loopback share, reaches no pipe, which the control's open says.
//
// What it costs when this fails: a link a less-trusted account planted on a path the program walks
// lets that account's pipe server, over the loopback share, act as the program.
func TestALinkToTheLoopbackPipesShareGivesItsServerIdentificationAlone(t *testing.T) {
	t.Parallel()
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
	for _, c := range []struct {
		name   string
		open   func(path string) (windows.Handle, error)
		writes bool   // whether the client writes a byte, which the server reads before it asks
		want   uint32 // the level a client that writes should give the server
	}{
		{"windows.CreateFile with no quality of service, the control", byPath(func(p *uint16) (windows.Handle, error) {
			return windows.CreateFile(p, rw, 0, nil, windows.OPEN_EXISTING, 0, 0)
		}), true, windows.SecurityImpersonation},
		{"CreateFile", byPath(func(p *uint16) (windows.Handle, error) {
			return CreateFile(p, rw, 0, windows.OPEN_EXISTING, 0)
		}), true, windows.SecurityIdentification},
		{"OpenFolder", func(path string) (windows.Handle, error) {
			f, err := OpenFolder(path)
			if err != nil {
				return 0, err
			}
			var h windows.Handle
			err = windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(f.Fd()), windows.CurrentProcess(), &h, 0, false, windows.DUPLICATE_SAME_ACCESS)
			return h, errors.Join(err, f.Close())
		}, false, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, name, server := pipeServer(t)
			link := filepath.Join(t.TempDir(), "link")
			l, err := windows.UTF16PtrFromString(link)
			if err != nil {
				t.Fatal(err)
			}
			target, err := windows.UTF16PtrFromString(`\\localhost\pipe\` + name)
			if err != nil {
				t.Fatal(err)
			}
			if err := windows.CreateSymbolicLink(l, target, windows.SYMBOLIC_LINK_FLAG_DIRECTORY); err != nil {
				t.Fatalf("make the link to the loopback pipes share (CI holds the right as an administrator): %v", err)
			}
			client, err := c.open(link)
			if err != nil {
				t.Fatalf("open through the link to the loopback pipes share: %v", err)
			}
			defer windows.CloseHandle(client)
			if err := windows.ConnectNamedPipe(server, nil); !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
				t.Fatalf("the open through the link reached no pipe: ConnectNamedPipe answered %v, not ERROR_PIPE_CONNECTED", err)
			}
			if !c.writes {
				level, err := clientLevel(server)
				t.Logf("native: through the link, the server of a client that wrote nothing took on its token at %s, %v", levels(level), err)
				if !errors.Is(err, windows.ERROR_CANNOT_IMPERSONATE) {
					t.Errorf("the server took on the token of a client that wrote nothing: %s, %v, and want ERROR_CANNOT_IMPERSONATE", levels(level), err)
				}
				return
			}
			level, err := levelAfterAByte(t, client, server)
			t.Logf("native: through the link, the server took on the client's token at %s, %v", levels(level), err)
			if err != nil || level != c.want {
				t.Errorf("the server took on the client's token at %s, %v, and want %s", levels(level), err, levels(c.want))
			}
		})
	}
}

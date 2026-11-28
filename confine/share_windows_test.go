package confine

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// TestARootOnAShareOfThisMachinesOwnDiskIsNotRefused opens a root on the temp folder reached by
// this machine's administrative share, \\localhost\C$\..., and shows that the refusal of a root on
// the pipes' device leaves it alone: the root's own handle is FILE_TYPE_DISK, so the file type, which
// decides where the system reads no final path, passes it, and the final path read from it is not on
// the pipes' device nor on a host's pipes share; Open, ReadCapped, IsRunnable and Names of a file in it
// each answer. A root on a share is a root on a disk, but on a host's pipes share, IPC$ or pipe, as
// TestARootOnAHostsPipesShareIsRefusedBeforeAnyOpenBeneathIt shows. Where the share cannot be
// reached the test fails, as it then shows nothing.
//
// What it costs when this fails: a program that keeps its files on a share, a mapped home folder
// among them, finds every call of confine refused as if its root were a pipe.
func TestARootOnAShareOfThisMachinesOwnDiskIsNotRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	vol := filepath.VolumeName(dir)
	if len(vol) != 2 {
		t.Fatalf("the control: the temp folder %s is on no drive letter, so no administrative share reaches it and this test shows nothing", dir)
	}
	share := `\\localhost\` + vol[:1] + `$` + dir[2:]
	root, err := os.OpenRoot(share)
	if err != nil {
		t.Fatalf("the control: os.OpenRoot of %s: %v, so this test shows nothing", share, err)
	}
	t.Cleanup(func() { _ = root.Close() })
	f, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	h := windows.Handle(f.Fd())
	if ft, err := windows.GetFileType(h); err != nil || ft != windows.FILE_TYPE_DISK {
		t.Errorf("the file type of the root %s: %d, %v, and want FILE_TYPE_DISK (%d)", share, ft, err, windows.FILE_TYPE_DISK)
	}
	var buf [windows.MAX_LONG_PATH]uint16
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), volumeNameNT)
	t.Logf("native: the final path of the root %s: %q, %v", share, windows.UTF16ToString(buf[:min(int(n), len(buf))]), err)
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rootOnPipes(root, "file"); err != nil {
		t.Errorf("rootOnPipes of the root %s: %v, and want it passed", share, err)
	}
	for _, s := range []struct {
		name string
		call func() error
	}{
		{"Open", func() error {
			f, err := Open(root, "file", os.O_RDONLY, 0)
			if f != nil {
				err = errors.Join(err, f.Close())
			}
			return err
		}},
		{"ReadCapped", func() error { _, err := ReadCapped(root, "file", 1); return err }},
		{"IsRunnable", func() error { _, err := IsRunnable(root, "file"); return err }},
		{"Names", func() error { _, err := Names(root, "file"); return err }},
	} {
		if err := s.call(); err != nil {
			t.Errorf("%s of a file in the root %s: %v, and want it answered", s.name, share, err)
		}
	}
}

// onPipesShare reads a path as on a host's pipes share where its share is IPC$ or pipe, in any case,
// for any host, in each spelling of a UNC path and in a final path under \Device\Mup\, a
// redirector's own components passed over; and a path on any other share, a share whose name only
// looks like one, IPC$ or pipe below another share, and a path on a disk, as on none.
//
// What it costs when this fails: a root on a host's pipes share passes rootOnPipes, and a name
// beneath it opens that host's pipe with no quality of service, so the pipe's server may act as the
// program; or, the other way, a root on a share of files is refused as if it were pipes.
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
		{`\\HOST.example\Ipc$`, true},
		{`\\localhost\pipe\`, true},
		{`\\[::1]\IPC$\`, true},
		{`\\?\UNC\h\PIPE\`, true},
		{`\\.\UNC\h\ipc$`, true},
		{`\??\UNC\h\pipe\x`, true},
		{`//h/ipc$/`, true},
		{`\\.\pipe\`, true},
		{`\Device\Mup\localhost\C$\x`, false},
		{`\Device\Mup\;LanmanRedirector\;Z:0000000000012345\h\data`, false},
		{`\\h\pipes\`, false},
		{`\\h\ipc\`, false},
		{`\\h\share\IPC$`, false},
		{`\\h\share\pipe\x`, false},
		{`\\h`, false},
		{`\\?\C:\IPC$`, false},
		{`\\.\C:\pipe`, false},
		{`C:\pipe\x`, false},
		{`\Device\HarddiskVolume3\pipe`, false},
		{``, false},
	} {
		if got := onPipesShare(c.path); got != c.want {
			t.Errorf("onPipesShare(%q): %t, and want %t", c.path, got, c.want)
		}
	}
}

// A root opened on a host's pipes share, IPC$ or pipe, by its UNC name in any spelling or through a
// folder link to it, holds that host's pipes as names beneath it. Each confine call that opens a name
// beneath a caller's root, rootCalls' list, refuses such a root, matching fs.ErrInvalid, from the
// root's own name or the final path read from its handle, and the pipe whose name each was given
// sees no client. Where the system opens no root on the share, or makes no link, that is logged as
// its own answer, and onPipesShare's table is the refusal's proof.
//
// What it costs when this fails: a root a program is given as \\host\IPC$, or one a link leads
// there, has confine open a name beneath it as that host's pipe, with no quality of service, so the
// pipe's server may act as the program.
func TestARootOnAHostsPipesShareIsRefusedBeforeAnyOpenBeneathIt(t *testing.T) {
	t.Parallel()
	disk := diskRoot(t)
	link := filepath.Join(t.TempDir(), "ipc")
	paths := []string{`\\localhost\IPC$\`, `\\127.0.0.1\pipe\`, `\\?\UNC\localhost\IPC$\`, link}
	if err := os.Symlink(`\\localhost\IPC$\`, link); err != nil {
		t.Logf("native: a folder link %s to \\\\localhost\\IPC$\\: %v", link, err)
		paths = paths[:3]
	}
	for _, path := range paths {
		root, err := os.OpenRoot(path)
		if err != nil {
			t.Logf("native: os.OpenRoot of %s: %v", path, err)
			continue
		}
		t.Cleanup(func() { _ = root.Close() })
		t.Logf("native: os.OpenRoot of %s opened", path)
		refusesEveryCall(t, root, path, disk)
	}
}

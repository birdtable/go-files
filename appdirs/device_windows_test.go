//go:build windows

package appdirs

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// listeningPipe makes the first instance of a new named pipe whose server never waits, and answers
// its name below \\.\pipe\ and reached, which is nil while no client has opened the pipe, and
// otherwise ConnectNamedPipe's answer, ERROR_PIPE_CONNECTED or, once the client closed it,
// ERROR_NO_DATA.
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

// openedByHand opens the pipe name with windows.CreateFile and closes it, the control that shows
// reached sees a client.
func openedByHand(t *testing.T, name string, reached func() error) {
	t.Helper()
	p, err := windows.UTF16PtrFromString(`\\.\pipe\` + name)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatalf("the control's open of the pipe %s: %v", name, err)
	}
	if err := windows.CloseHandle(h); err != nil {
		t.Fatal(err)
	}
	if reached() == nil {
		t.Fatal("the control: the pipe opened by hand reads as never opened, so this test cannot see an open")
	}
}

// A \\.\ path and a \\?\ or \??\ path that names neither a drive nor a Volume{GUID} name a device
// or a pipe, not a file or a folder on a volume, and an open of a pipe tells its server who the
// caller is: Volume, Free and a folder Temp keeps apart refuse each, matching fs.ErrInvalid, before
// anything is opened, and Temp makes nothing. The pipe's server, which never waits, is asked after
// them whether a client came; a control opens a second pipe by hand and is seen.
//
// What it costs when this fails: a path a less-trusted account wrote leads appdirs' open to that
// account's pipe, whose server learns which account runs the program.
func TestAPathToAPipeOrADeviceIsRefusedBeforeAnyOpen(t *testing.T) {
	t.Parallel()
	name, reached := listeningPipe(t)
	dir := filepath.Join(testDir(t), "temp")
	refused := func(what string, err error) {
		t.Helper()
		switch {
		case err == nil:
			t.Errorf("%s: no error, and want a refusal", what)
		case !errors.Is(err, fs.ErrInvalid):
			t.Errorf("%s: %v, and want fs.ErrInvalid", what, err)
		case !strings.Contains(err.Error(), "names a device or a pipe"):
			t.Errorf("%s: %v does not say it names a device or a pipe", what, err)
		}
	}
	for _, p := range []string{`\\.\pipe\` + name, `\\?\pipe\` + name, `\??\pipe\` + name, `\\.\NUL`, `\\?\GLOBALROOT\Device\NamedPipe\` + name} {
		_, err := Volume(p)
		refused("Volume of "+p, err)
		_, err = Free(p)
		refused("Free of "+p, err)
		_, err = temp(dir, []string{p}, nil, false)
		refused("temp apart from "+p, err)
	}
	if _, err := os.Lstat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the temp folder %s after the refusals: %v, and want it not made", dir, err)
	}
	if err := reached(); err != nil {
		t.Error(err)
	}
	other, seen := listeningPipe(t)
	openedByHand(t, other, seen)
}

// A junction any account may plant can name a pipe, \Device\NamedPipe\<name> or \??\pipe\<name>,
// or the pipes' own folder with the pipe's name below it in the path. Volume, Free and a folder
// Temp keeps apart each refuse one: none answers a pipe as a folder, and each open that follows the
// name passes winapi.Identify, so a pipe it reaches lets its server learn who the caller is and act
// as it in nothing (internal/winapi's tests show the level). Volume and Free read the volume's root
// from their own handle, never from the path again. Whether each reaches the pipe is logged, as
// this system's own answer, with what os.Stat does.
//
// What it costs when this fails: a junction planted where the program asks of a volume, or names a
// folder to keep its temp folder apart from, passes a pipe as a folder, or lets its server act as
// the program.
func TestAJunctionToAPipeIsNoFolderToAppdirs(t *testing.T) {
	t.Parallel()
	dir := testDir(t)
	sites := []struct {
		name string
		call func(path string) error
	}{
		{"os.Stat, the control", func(p string) error { _, err := os.Stat(p); return err }},
		{"Volume", func(p string) error { _, err := Volume(p); return err }},
		{"Free", func(p string) error { _, err := Free(p); return err }},
		{"temp apart", func(p string) error {
			_, err := temp(filepath.Join(dir, "t-"+rand.Text()), []string{p}, nil, false)
			return err
		}},
	}
	for _, c := range []struct {
		name string
		sub  func(pipe string) string
		path func(junction, pipe string) string
	}{
		{`\Device\NamedPipe\<name>`, func(n string) string { return `\Device\NamedPipe\` + n }, func(j, _ string) string { return j }},
		{`\??\pipe\<name>`, func(n string) string { return `\??\pipe\` + n }, func(j, _ string) string { return j }},
		{`\Device\NamedPipe\, the name below it`, func(string) string { return `\Device\NamedPipe\` }, func(j, n string) string { return filepath.Join(j, n) }},
	} {
		for _, s := range sites {
			name, reached := listeningPipe(t)
			j := filepath.Join(dir, "j-"+rand.Text())
			if err := plantLink(j, c.sub(name), c.sub(name)); err != nil {
				t.Logf("native: the system refuses the junction %s: %v", c.name, err)
				break
			}
			p := c.path(j, name)
			err := s.call(p)
			t.Logf("native: %s through the junction %s: %v; %v", s.name, c.name, err, reachedSays(reached()))
			if err == nil && s.name != "os.Stat, the control" {
				t.Errorf("%s of %s through the junction %s: no error, and a pipe is no folder to it", s.name, p, c.name)
			}
		}
	}
}

// reachedSays is a listeningPipe's reached answer said for the log.
func reachedSays(err error) string {
	if err == nil {
		return "the pipe saw no client"
	}
	return "the pipe was opened (" + err.Error() + ")"
}

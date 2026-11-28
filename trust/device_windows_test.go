//go:build windows

package trust

import (
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
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

// A \\.\ path and a \\?\ path that names neither a drive nor a Volume{GUID} name a device or a
// pipe, not a file on a volume, and an open of a pipe tells its server who the caller is: each is
// refused, matching ErrNotPlain, before anything is opened, where it is the path walked, a link's
// target, a folder kept apart, the file ReadOwned reads and the folder MkdirPrivate makes. A
// junction to \??\pipe\ is read, never followed, so its target is refused as a path. The pipe's
// server, which never waits, is asked after each whether a client came; a control opens a second
// pipe by hand and is seen.
//
// What it costs when this fails: a path a less-trusted account wrote, or a junction it planted,
// leads trust's open to that account's pipe, whose server learns which account runs the program
// and holds the open as long as it likes.
func TestAPathToAPipeOrADeviceIsRefusedBeforeAnyOpen(t *testing.T) {
	t.Parallel()
	name, reached := listeningPipe(t)
	d := nativeDir(t)
	file := filepath.Join(d, "f")
	newFile(t, file)
	says := refusal{want: ErrNotPlain, says: "names a device or a pipe"}
	for _, p := range []string{`\\.\pipe\` + name, `\\?\pipe\` + name, `\\.\NUL`, `\\?\GLOBALROOT\Device\NamedPipe\` + name} {
		_, err := Path(p, File, Who{})
		wantRefused(t, "Path of "+p, err, says)
		_, err = Path(file, File, Who{Apart: []string{p}})
		wantRefused(t, "a folder kept apart, "+p, err, says)
		_, err = ReadOwned(p, me(t), false, 5)
		wantRefused(t, "ReadOwned of "+p, err, says)
		root, err := MkdirPrivate(p, Who{})
		if root != nil {
			_ = root.Close()
		}
		wantRefused(t, "MkdirPrivate of "+p, err, says)
	}
	j := filepath.Join(d, "j")
	junction(t, j, `pipe\`+name)
	_, err := Path(j, File, Who{})
	wantRefused(t, "a junction to the pipe", err, says)
	if err := reached(); err != nil {
		t.Error(err)
	}
	other, seen := listeningPipe(t)
	openedByHand(t, other, seen)
}

// MkdirPrivate looks for the part of its folder that is there before it judges anything, by
// nameThere, whose open of each name asks for identification alone, as trust's every CreateFile
// does, and never by os.Lstat, whose opens let a pipe's server act as the caller. A junction any
// account may plant to the pipes' own folder, with the pipe's name below it in the folder asked
// for, leads that look to the pipe: MkdirPrivate refuses the folder all the same, as Path's walk
// refuses the junction, and whether the look reached the pipe is logged as this system's answer.
// The level an open with these flags gives a pipe's server is shown by internal/winapi's tests.
//
// What it costs when this fails: a folder a less-trusted account planted a junction in is made, or
// the look that comes before the judgement lets the pipe's server act as the program.
func TestAJunctionToAPipeInTheFolderMkdirPrivateMakesIsRefused(t *testing.T) {
	t.Parallel()
	name, reached := listeningPipe(t)
	j := filepath.Join(nativeDir(t), "j")
	junction(t, j, `GLOBALROOT\Device\NamedPipe\`)
	dir := filepath.Join(j, name, "sub")
	root, err := MkdirPrivate(dir, Who{})
	if root != nil {
		_ = root.Close()
	}
	t.Logf("native: MkdirPrivate of %s through a junction to the pipes' folder: %v; %v", dir, err, reachedSays(reached()))
	if err == nil {
		t.Errorf("MkdirPrivate of %s: no error, and a junction to the pipes' folder is on its way", dir)
	}
}

// reachedSays is a listeningPipe's reached answer said for the log.
func reachedSays(err error) string {
	if err == nil {
		return "the pipe saw no client"
	}
	return "the pipe was opened (" + err.Error() + ")"
}

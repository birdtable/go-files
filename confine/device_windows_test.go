package confine

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
// or a pipe, not a file on a volume, and an open of a pipe tells its server who the caller is:
// Resolve, Below's folder and its path, Nearest's start, Folds' root and the target of LinkFolder
// and LinkFile refuse each, matching fs.ErrInvalid, before anything is opened. The pipe's server,
// which never waits, is asked after them whether a client came; a control opens a second pipe by
// hand and is seen.
//
// What it costs when this fails: a path a less-trusted account wrote leads confine's open to that
// account's pipe, whose server learns which account runs the program.
func TestAPathToAPipeOrADeviceIsRefusedBeforeAnyOpen(t *testing.T) {
	t.Parallel()
	name, reached := listeningPipe(t)
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	refused := func(what string, err error, outside bool) {
		t.Helper()
		switch {
		case err == nil:
			t.Errorf("%s: no error, and want a refusal", what)
		case !errors.Is(err, fs.ErrInvalid) || outside && !errors.Is(err, ErrOutside):
			t.Errorf("%s: %v, and want fs.ErrInvalid (and ErrOutside: %v)", what, err, outside)
		case !strings.Contains(err.Error(), "names a device or a pipe"):
			t.Errorf("%s: %v does not say it names a device or a pipe", what, err)
		}
	}
	for i, p := range []string{`\\.\pipe\` + name, `\\?\pipe\` + name, `\??\pipe\` + name, `\\.\NUL`, `\\?\GLOBALROOT\Device\NamedPipe\` + name} {
		_, err := Resolve(p)
		refused("Resolve of "+p, err, true)
		_, err = Below(p, dir)
		refused("Below of the folder "+p, err, true)
		_, err = Below(dir, p)
		refused("Below of the path "+p, err, true)
		_, err = Nearest(p, "go.mod", "")
		refused("Nearest from "+p, err, true)
		_, err = Folds(p)
		refused("Folds of "+p, err, false)
		refused("LinkFolder to "+p, LinkFolder(root, p, fmt.Sprint("d", i)), false)
		refused("LinkFile to "+p, LinkFile(root, p, fmt.Sprint("f", i)), false)
	}
	if err := reached(); err != nil {
		t.Error(err)
	}
	other, seen := listeningPipe(t)
	openedByHand(t, other, seen)
}

// A junction any account may plant can name a pipe, \Device\NamedPipe\<name> or \??\pipe\<name>,
// or the pipes' own folder with the pipe's name below it in the path. Each of confine's calls that
// takes a path refuses one: none answers a pipe as a folder or a file, a \??\pipe\ target is
// refused as a device path where the walk reads it, and the opens that follow a name are
// winapi.Stat and winapi.OpenPath, which reach a pipe at identification alone, so its server
// learns who the caller is and may act as it in nothing (internal/winapi's tests show the level).
// Whether each reaches the pipe is logged, as this system's own answer, with what os.Stat does.
// Where the path goes on below the junction to the pipes' folder, the system opens the pipe, so
// each refusal there is confine's own, matching fs.ErrInvalid or ErrOutside.
// Nearest takes the junction itself as its marker where the path ends at it, as it takes any link
// there, and that look must not reach the pipe; a marker below it that is the pipe is refused.
//
// What it costs when this fails: a junction planted in a folder the program resolves, copies or
// links from passes a pipe as a folder or a file, or lets its server act as the program.
func TestAJunctionToAPipeIsNoFolderOrFileToConfine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	sites := []struct {
		name string
		call func(path string) error
	}{
		{"os.Stat, the control", func(p string) error { _, err := os.Stat(p); return err }},
		{"Resolve", func(p string) error { _, err := Resolve(p); return err }},
		{"Below's path", func(p string) error { _, err := Below(dir, p); return err }},
		{"Below's folder", func(p string) error { _, err := Below(p, dir); return err }},
		{"Folds", func(p string) error { _, err := Folds(p); return err }},
		{"Nearest", func(p string) error { _, err := Nearest(filepath.Dir(p), filepath.Base(p), ""); return err }},
		{"LinkFolder", func(p string) error { return LinkFolder(root, p, "d-"+rand.Text()) }},
		{"LinkFile", func(p string) error { return LinkFile(root, p, "f-"+rand.Text()) }},
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
			if err := plantRawJunction(j, c.sub(name), c.sub(name)); err != nil {
				t.Logf("native: the system refuses the junction %s: %v", c.name, err)
				break
			}
			p := c.path(j, name)
			err := s.call(p)
			r := reached()
			t.Logf("native: %s through the junction %s: %v; %v", s.name, c.name, err, reachedSays(r))
			switch {
			case s.name == "Nearest" && p == j:
				if r != nil {
					t.Errorf("Nearest of the junction %s itself as the marker reached the pipe: %v", c.name, r)
				}
			case s.name == "os.Stat, the control":
			case err == nil:
				t.Errorf("%s of %s through the junction %s: no error, and a pipe is no folder or file to it", s.name, p, c.name)
			case p != j && !errors.Is(err, fs.ErrInvalid) && !errors.Is(err, ErrOutside):
				t.Errorf("%s of %s through the junction %s: %v, and want its own refusal, matching fs.ErrInvalid or ErrOutside", s.name, p, c.name, err)
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

// Resolve's walk looks at each name by winapi.Lstat and reads each link by winapi.Readlink, whose
// opens ask for identification alone, never by os.Lstat and os.Readlink, whose opens let a pipe's
// server act as the caller (internal/winapi's tests show the level). A folder the walk has passed
// may change under it: here it becomes a junction to the pipes' own folder as the walk looks at
// the pipe's name below it, so the walk's look lands on the pipe. Resolve refuses the path all the
// same, as the pipe is no file it answers, and whether the walk reached the pipe is logged as this
// system's answer. The control resolves the path before the swap and finds its last name not there.
//
// What it costs when this fails: a junction planted under a walk that has passed its folder
// answers a pipe as a file, or lets the pipe's server act as the program.
func TestAFolderTheWalkPassedTurnedToAPipeIsNoFileToResolve(t *testing.T) {
	t.Parallel()
	name, reached := listeningPipe(t)
	a := filepath.Join(t.TempDir(), "a")
	if err := os.Mkdir(a, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a, name)
	if _, rest, err := resolveChecked(path, func() {}); err != nil || len(rest) != 1 {
		t.Fatalf("the control: Resolve of %s before the swap: rest %v, %v, and want its last name not there", path, rest, err)
	}
	swapped := false
	got, rest, err := resolveSwapped(path, func(next string) {
		if swapped || !strings.HasSuffix(next, `\a\`+name) {
			return
		}
		swapped = true
		if err := os.Remove(a); err != nil {
			t.Fatal(err)
		}
		if err := plantRawJunction(a, `\Device\NamedPipe\`, `\Device\NamedPipe\`); err != nil {
			t.Fatalf("make %s a junction to the pipes' folder: %v", a, err)
		}
	}, func() {})
	t.Logf("native: Resolve of %s, its folder turned to a junction to the pipes' folder as the walk looked at the pipe's name: %q, rest %v, %v; %v", path, got, rest, err, reachedSays(reached()))
	switch {
	case !swapped:
		t.Fatal("the walk never looked at the pipe's name, so the folder was never swapped")
	case err == nil:
		t.Errorf("Resolve of %s: %q, rest %v, and no error, and a pipe is no file to it", path, got, rest)
	}
}

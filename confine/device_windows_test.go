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
// Resolve, Below's folder and its path, Nearest's start, Folds' root, Final's path and the target
// of LinkFolder and LinkFile refuse each, matching fs.ErrInvalid, before anything is opened. The pipe's server,
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
		_, err = Final(p)
		refused("Final of "+p, err, true)
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
// there, and Final opens the junction itself there, as it opens a link at the last name, and
// neither look may reach the pipe; a marker below it that is the pipe is refused, and so is Final
// of the pipe below it, which its open reaches at identification alone.
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
		{"Final", func(p string) error { _, err := Final(p); return err }},
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
			case (s.name == "Nearest" || s.name == "Final") && p == j:
				if r != nil {
					t.Errorf("%s of the junction %s itself reached the pipe: %v", s.name, c.name, r)
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

// A root opened on the pipes' folder, by os.OpenRoot through a junction to \Device\NamedPipe\ or of
// \\.\pipe\ itself, holds each pipe as a name beneath it, and Go's open within a root passes no
// quality of service. Each confine call that opens a name beneath a caller's root, rootCalls' list,
// refuses such a root, matching fs.ErrInvalid, read from the root's own handle before any open
// beneath it, and the pipe whose name each was given sees no client. The control opens a second
// pipe's name by the root's own OpenFile and is seen, so the route is live; where the system opens
// no root on \\.\pipe\ itself, or no name within it, that is logged as its own answer. A root on a
// disk is unchanged, as every other test of these calls shows.
//
// What it costs when this fails: a junction planted where the program opens a root leads confine's
// open, removal, folder or link of a name beneath it to a less-trusted account's pipe, whose server
// may then act as the program.
func TestARootOnThePipesFolderIsRefusedBeforeAnyOpenBeneathIt(t *testing.T) {
	t.Parallel()
	j := filepath.Join(t.TempDir(), "pipes")
	if err := plantRawJunction(j, `\Device\NamedPipe\`, `\Device\NamedPipe\`); err != nil {
		t.Fatalf("plant the junction %s to the pipes' folder: %v", j, err)
	}
	disk := diskRoot(t)
	for _, c := range []struct {
		name, path string
		must       bool
	}{
		{`through a junction to \Device\NamedPipe\`, j, true},
		{`of \\.\pipe\ itself`, `\\.\pipe\`, false},
	} {
		root, err := os.OpenRoot(c.path)
		switch {
		case err != nil && c.must:
			t.Fatalf("the control: os.OpenRoot of the pipes' folder %s: %v", c.name, err)
		case err != nil:
			t.Logf("native: os.OpenRoot of the pipes' folder %s: %v", c.name, err)
			continue
		}
		t.Cleanup(func() { _ = root.Close() })
		other, seen := listeningPipe(t)
		f, err := root.OpenFile(other, os.O_RDWR, 0)
		if err == nil {
			err = f.Close()
		}
		saw := seen() != nil
		switch {
		case (err != nil || !saw) && c.must:
			t.Fatalf("the control: the root's own open of a pipe's name in the root %s: %v, and the pipe saw a client: %t, so this test cannot see an open", c.name, err, saw)
		case err != nil || !saw:
			t.Logf("native: the root's own open of a pipe's name in the root %s: %v", c.name, err)
			continue
		}
		refusesEveryCall(t, root, c.name, disk)
	}
}

// rootCall is one confine call that opens a name beneath a caller's root, given that root and the
// name.
type rootCall struct {
	name string
	call func(root *os.Root, name string) error
}

// rootCalls are the confine calls that open a name beneath a caller's root, each of which asks
// rootOnPipes first, by walkOpen or at its own entry. disk is a root on a disk, the target of the
// links and CopyTree's other root where it is copied from; linked, a root on a disk that holds only
// a junction to itself, is CopyTree's other root where it is copied into, so the copy has a link and
// nothing else to make there.
func rootCalls(disk, linked *os.Root) []rootCall {
	closed := func(r *os.Root, err error) error {
		if r != nil {
			err = errors.Join(err, r.Close())
		}
		return err
	}
	return []rootCall{
		{"Open", func(r *os.Root, n string) error {
			f, err := Open(r, n, os.O_RDWR, 0)
			if f != nil {
				err = errors.Join(err, f.Close())
			}
			return err
		}},
		{"ReadCapped", func(r *os.Root, n string) error { _, err := ReadCapped(r, n, 1); return err }},
		{"IsRunnable", func(r *os.Root, n string) error { _, err := IsRunnable(r, n); return err }},
		{"Names", func(r *os.Root, n string) error { _, err := Names(r, n); return err }},
		{"NoLinks", func(r *os.Root, n string) error { _, err := NoLinks(r, n); return err }},
		{"Readlink", func(r *os.Root, n string) error { _, err := Readlink(r, n); return err }},
		{"Remove", func(r *os.Root, n string) error { return Remove(r, n) }},
		{"RemoveAll", func(r *os.Root, n string) error { return RemoveAll(r, n) }},
		{"RemoveAllOwned", func(r *os.Root, n string) error { return RemoveAllOwned(r, n) }},
		{"Mkdir", func(r *os.Root, n string) error { return closed(Mkdir(r, n, 0o700)) }},
		{"MkdirPrivate", func(r *os.Root, n string) error { return closed(MkdirPrivate(r, n)) }},
		{"MkdirShared", func(r *os.Root, n string) error { return closed(MkdirShared(r, n)) }},
		{"MkdirFresh", func(r *os.Root, n string) error { d, _, err := MkdirFresh(r, n); return closed(d, err) }},
		{"Collide", func(r *os.Root, n string) error { _, err := Collide(r, n, n+"-b"); return err }},
		{"RenameNoReplace", func(r *os.Root, n string) error { return RenameNoReplace(r, n, n+"-to") }},
		{"RenameExchange", func(r *os.Root, n string) error { return RenameExchange(r, n, n+"-to") }},
		{"LinkFolder", func(r *os.Root, n string) error { return LinkFolder(r, disk.Name(), n) }},
		{"LinkFile", func(r *os.Root, n string) error { return LinkFile(r, filepath.Join(disk.Name(), "file"), n) }},
		{"CopyTree from it", func(r *os.Root, _ string) error { return CopyTree(disk, r) }},
		{"CopyTree into it", func(r *os.Root, _ string) error { return CopyTree(r, linked) }},
	}
}

// diskRoot is a root on a disk, a temporary folder holding the file "file", closed at the test's end.
func diskRoot(t *testing.T) *os.Root {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

// linkedRoot is a root on a disk, a temporary folder holding only the junction "j" to itself, closed
// at the test's end.
func linkedRoot(t *testing.T) *os.Root {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	if err := LinkFolder(root, dir, "j"); err != nil {
		t.Fatalf("the control: LinkFolder of j to %s: %v", dir, err)
	}
	return root
}

// refusesEveryCall gives each of rootCalls the name of a pipe of its own, listening, beneath root,
// and wants each refused, matching fs.ErrInvalid, with the pipe seeing no client. where says what
// root is.
func refusesEveryCall(t *testing.T, root *os.Root, where string, disk *os.Root) {
	t.Helper()
	for _, s := range rootCalls(disk, linkedRoot(t)) {
		name, reached := listeningPipe(t)
		err := s.call(root, name)
		switch {
		case err == nil:
			t.Errorf("%s of the pipe %s in the root %s: no error, and want the root refused", s.name, name, where)
		case !errors.Is(err, fs.ErrInvalid):
			t.Errorf("%s of the pipe %s in the root %s: %v, and want the root refused, matching fs.ErrInvalid", s.name, name, where, err)
		}
		if err := reached(); err != nil {
			t.Errorf("%s of the pipe %s in the root %s: %v", s.name, name, where, err)
		}
	}
}

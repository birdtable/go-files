//go:build unix

package trust

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// The tests of this file walk names on this machine's disk, made by this account, which is not
// root: the test tier runs as an account that is not.

// realDir is a new folder of the test's, with no link on its way, which the walk passes.
func realDir(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Fatal("run as root, which every rule trusts: the test tier runs as an account that is not root")
	}
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Path(d, Folder, Who{NoLinks: true}); err != nil {
		t.Fatalf("the test's own folder does not pass, so no case here says anything: %v", err)
	}
	return d
}

// mk writes the name p of the kind k, with the permission bits perm, set after the umask.
func mk(t *testing.T, p string, k Kind, perm fs.FileMode) {
	t.Helper()
	var err error
	switch k {
	case Folder, PrivateFolder:
		err = os.Mkdir(p, 0o700)
	case Socket:
		err = socket(t, p)
	default:
		err = os.WriteFile(p, []byte("x"), 0o600)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, perm); err != nil {
		t.Fatal(err)
	}
}

// socket binds a Unix socket at p. A socket's name must fit the kernel's 104 bytes, which p need
// not, so it is bound through a link, in a folder of this account's in /tmp, to p's folder: the
// kernel follows the link and makes the socket at p, on p's own file system, with no rename.
func socket(t *testing.T, p string) error {
	t.Helper()
	tmp, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		return err
	}
	short, err := os.MkdirTemp(tmp, "ts")
	if err != nil {
		return err
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(short); err != nil {
			t.Error(err)
		}
	})
	if err := os.Symlink(filepath.Dir(p), filepath.Join(short, "d")); err != nil {
		return err
	}
	l, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", filepath.Join(short, "d", filepath.Base(p)))
	if err != nil {
		return err
	}
	if u, ok := l.(*net.UnixListener); ok {
		u.SetUnlinkOnClose(false) // p is the test's to remove, with its folder
	}
	t.Cleanup(func() {
		if err := l.Close(); err != nil {
			t.Error(err)
		}
	})
	return nil
}

// applyFix makes fix on path as a caller would: it takes Clear from the mode and adds Set. A fix
// that names another path, or asks for more than a change of mode, fails the test.
func applyFix(t *testing.T, fix Fix, path string) {
	t.Helper()
	if fix.Path != path || fix.Owner != "" || fix.Revoke != "" || fix.Reset || fix.Clear|fix.Set == 0 {
		t.Fatalf("the fix %+v is not a change of mode at %s", fix, path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	m := info.Mode() & (fs.ModePerm | fs.ModeSticky)
	if err := os.Chmod(path, m&^fix.Clear|fix.Set); err != nil {
		t.Fatal(err)
	}
}

// What it costs when this fails: on a real disk a name of each kind that another account can
// change, or a key it can read, passes, and a program runs or reads what that account wrote; or
// the fix a refusal names does not make the name pass.
func TestEachKindOnThisDiskIsRefusedByItsModeAndPassesOnceItsFixIsRun(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	cases := []struct {
		name string
		k    Kind
		perm fs.FileMode
		want error
	}{
		{"file", File, 0o644, nil},
		{"file-group-writes", File, 0o664, ErrOthersWrite},
		{"file-others-write", File, 0o646, ErrOthersWrite},
		{"key", Key, 0o600, nil},
		{"key-others-read", Key, 0o604, ErrOthersRead},
		{"key-group-reads", Key, 0o640, ErrOthersRead},
		{"private", PrivateFolder, 0o700, nil},
		{"private-group-reads", PrivateFolder, 0o750, ErrOthersRead},
		{"private-others-read", PrivateFolder, 0o705, ErrOthersRead},
		{"program", Program, 0o755, nil},
		{"program-not-executable", Program, 0o644, nil},
		{"program-group-writes", Program, 0o775, ErrOthersWrite},
		{"folder", Folder, 0o755, nil},
		{"folder-group-writes", Folder, 0o775, ErrOthersWrite},
		{"folder-others-write", Folder, 0o757, ErrOthersWrite},
		{"s", Socket, 0o755, nil},
		{"s-others-write", Socket, 0o757, ErrOthersWrite},
	}
	for _, c := range cases {
		p := filepath.Join(d, c.name)
		mk(t, p, c.k, c.perm)
		_, err := Path(p, c.k, Who{})
		u, refused := errors.AsType[*Unsafe](err)
		switch {
		case !refused && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case refused != (c.want != nil || strings.HasSuffix(c.name, "not-executable")):
			t.Errorf("%s: %v, and want %v", c.name, err, c.want)
		case refused && c.want != nil && !errors.Is(err, c.want):
			t.Errorf("%s: %v, and want %v", c.name, err, c.want)
		case refused:
			applyFix(t, u.Fix, p)
			if _, err := Path(p, c.k, Who{}); err != nil {
				t.Errorf("%s: after %+v, %v", c.name, u.Fix, err)
			}
		}
	}
}

// What it costs when this fails: root alone trusts a name of this account's, and a program root
// runs reads what this account wrote. The walk may refuse a folder above first (on Linux the
// test's folder is in /tmp, which root alone refuses as one others can write), so the file's own
// facts, read from this disk, are judged as the last name too.
func TestRootAloneRefusesANameOfThisAccountsOnThisDisk(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	p := filepath.Join(d, "f")
	mk(t, p, File, 0o644)
	if _, err := Path(p, File, Who{RootOnly: true}); !errors.As(err, new(*Unsafe)) {
		t.Errorf("the path, root alone trusted: %v, and want an *Unsafe", err)
	}
	n, err := look(p)
	if err != nil || n.owner != local().me {
		t.Fatalf("the owner of a file this account made: %q, %v, and want %s", n.owner, err, local().me)
	}
	err = judge(n, File, place{path: p, last: true}, Who{RootOnly: true}, local())
	wantRefused(t, "the file, root alone trusted", err, refusal{want: ErrNotOwners, fix: Fix{Path: p, Owner: "uid:0"}, says: n.owner})
}

// What it costs when this fails: a link on the way is followed where the caller said none may
// be, or a path is answered with a link left in it.
func TestALinkOnThisDiskIsFollowedOrRefusedAsAsked(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	f, sub, l, ld := filepath.Join(d, "f"), filepath.Join(d, "sub"), filepath.Join(d, "l"), filepath.Join(d, "ld")
	mk(t, f, File, 0o644)
	mk(t, sub, Folder, 0o755)
	mk(t, filepath.Join(sub, "g"), File, 0o644)
	for target, name := range map[string]string{"f": l, "sub": ld} {
		if err := os.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := Path(l, File, Who{}); err != nil || got != f {
		t.Errorf("a link at the end: %q, %v, and want %q", got, err, f)
	}
	if got, err := Path(filepath.Join(ld, "g"), File, Who{}); err != nil || got != filepath.Join(sub, "g") {
		t.Errorf("a link in the middle: %q, %v, and want %q", got, err, filepath.Join(sub, "g"))
	}
	for _, p := range []string{l, filepath.Join(ld, "g")} {
		_, err := Path(p, File, Who{NoLinks: true})
		if u, ok := errors.AsType[*Unsafe](err); !ok || !errors.Is(err, ErrNotPlain) || !strings.HasPrefix(u.Observed, filepath.Dir(p)) {
			t.Errorf("%s with NoLinks: %v, and want ErrNotPlain at its link", p, err)
		}
	}
}

// What it costs when this fails: a folder its group can write passes when the caller did not let
// that group in, or is refused when it did, and a package manager's shared folder cannot be used.
func TestAFolderItsGroupCanWritePassesOnlyWhenGroupLetsItsGroupIn(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	p := filepath.Join(d, "shared")
	mk(t, p, Folder, 0o775)
	info, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	gid := info.Sys().(*syscall.Stat_t).Gid
	if _, err := Path(p, Folder, Who{Group: func(g uint32) bool { return g == gid }}); err != nil {
		t.Errorf("its own group let in: %v", err)
	}
	for name, who := range map[string]Who{"no group": {}, "another group": {Group: func(g uint32) bool { return g != gid }},
		"root alone": {RootOnly: true, Group: func(uint32) bool { return true }}} {
		if _, err := Path(p, Folder, who); !errors.Is(err, ErrOthersWrite) && !errors.Is(err, ErrNotOwners) {
			t.Errorf("%s: %v, and want it refused", name, err)
		}
	}
}

// What it costs when this fails: /tmp, root's sticky folder, refuses every path beneath it, or a
// file right in it, where another account can add a hard link, passes; or a folder others can
// write that is not root's sticky folder passes.
func TestAWorldWritableFolderPassesOnlyWithTheStickyBitAndOnlyAboveTheLastName(t *testing.T) {
	t.Parallel()
	tmp, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(tmp); err != nil || info.Mode()&fs.ModeSticky == 0 || info.Sys().(*syscall.Stat_t).Uid != 0 {
		t.Fatalf("%s is not root's sticky folder here (%v), and the test needs it", tmp, err)
	}
	mine, err := os.MkdirTemp(tmp, "trust-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(mine); err != nil {
			t.Error(err)
		}
	})
	f := filepath.Join(mine, "f")
	mk(t, f, File, 0o644)
	if _, err := Path(f, File, Who{}); err != nil {
		t.Errorf("a file in a folder of this account's in %s: %v", tmp, err)
	}
	if _, err := Path(f, File, Who{RootOnly: true}); err == nil {
		t.Errorf("root alone passed a path through %s", tmp)
	}
	if _, err := Path(tmp, Folder, Who{}); !errors.Is(err, ErrOthersWrite) {
		t.Errorf("%s as the last name: %v, and want ErrOthersWrite", tmp, err)
	}
	right, err := os.CreateTemp(tmp, "trust-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := errors.Join(right.Close(), os.Remove(right.Name())); err != nil {
			t.Error(err)
		}
	})
	if _, err := Path(right.Name(), File, Who{}); !errors.Is(err, ErrNotOwners) {
		t.Errorf("a file right in %s: %v, and want ErrNotOwners", tmp, err)
	}
	for _, perm := range []fs.FileMode{0o777, fs.ModeSticky | 0o777} {
		if err := os.Chmod(mine, perm); err != nil {
			t.Fatal(err)
		}
		if _, err := Path(f, File, Who{}); !errors.Is(err, ErrOthersWrite) {
			t.Errorf("a file below a folder of this account's of mode %v: %v, and want ErrOthersWrite", perm, err)
		}
	}
}

// What it costs when this fails: ReadOwned reads a pipe, a link, a file another account can
// change, a private file others can read, or a file over its limit, and its caller takes what
// another wrote, waits on a writer, or holds more than it planned for.
func TestReadOwnedRefusesEachFileItMustNotRead(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	me := "uid:" + strconv.Itoa(os.Geteuid())
	in := func(name string) string { return filepath.Join(d, name) }
	mk(t, in("ok"), File, 0o644)
	mk(t, in("group-writes"), File, 0o664)
	mk(t, in("private"), File, 0o644)
	mk(t, in("folder"), Folder, 0o755)
	mk(t, in("s"), Socket, 0o755)
	if err := syscall.Mkfifo(in("pipe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("ok", in("link")); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, owner string
		private     bool
		limit       int
		want        error
	}{
		{"ok", me, false, 1, nil},
		{"ok", me, true, 1, ErrOthersRead},
		{"ok", me, false, 0, nil},
		{"ok", "uid:0", false, 1, ErrNotOwners},
		{"ok", "root", false, 1, nil},
		{"ok", "uid:01", false, 1, nil},
		{"group-writes", me, false, 1, ErrOthersWrite},
		{"private", me, true, 1, ErrOthersRead},
		{"folder", me, false, 1, ErrNotPlain},
		{"s", me, false, 1, ErrNotPlain},
		{"pipe", me, false, 1, ErrNotPlain},
		{"link", me, false, 1, ErrNotPlain},
	}
	for _, c := range cases {
		b, err := ReadOwned(in(c.name), c.owner, c.private, c.limit)
		pass := c.want == nil && c.limit == 1 && c.owner == me && !c.private
		switch {
		case pass && (err != nil || string(b) != "x"):
			t.Errorf("%s: %q, %v, and want it read", c.name, b, err)
		case !pass && b != nil:
			t.Errorf("%s as %s, limit %d: read %q", c.name, c.owner, c.limit, b)
		case !pass && c.want != nil && !errors.Is(err, c.want):
			t.Errorf("%s as %s: %v, and want %v", c.name, c.owner, err, c.want)
		case !pass && c.want == nil:
			// refused before anything is opened, so matching none of the refusals of a file
			if u, ok := errors.AsType[*Unsafe](err); !ok || u.Unwrap() != nil {
				t.Errorf("%s as %s, limit %d: %v, and want an *Unsafe matching no refusal of a file", c.name, c.owner, c.limit, err)
			}
		}
	}
	for _, name := range []string{"group-writes", "private"} {
		_, err := ReadOwned(in(name), me, true, 1)
		u, _ := errors.AsType[*Unsafe](err)
		if u == nil {
			t.Fatalf("%s: %v", name, err)
		}
		applyFix(t, u.Fix, in(name))
		if _, err := ReadOwned(in(name), me, true, 1); err != nil {
			t.Errorf("%s: after %+v, %v", name, u.Fix, err)
		}
	}
}

// What it costs when this fails: a file one byte over its limit is read, and a caller holds more
// than it planned for; or a file at its limit is refused.
func TestReadOwnedRefusesTheSmallestFileOverItsLimit(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	me := "uid:" + strconv.Itoa(os.Geteuid())
	p := filepath.Join(d, "f")
	if err := os.WriteFile(p, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := ReadOwned(p, me, true, 10); err != nil || string(b) != "0123456789" {
		t.Errorf("at its limit: %q, %v", b, err)
	}
	// refused by the size on the descriptor, before a byte is read
	if b, err := ReadOwned(p, me, true, 9); !errors.Is(err, ErrTooLong) || b != nil || !strings.Contains(err.Error(), "holds 10 bytes, over 9") {
		t.Errorf("one byte over: %q, %v, and want ErrTooLong at its stat", b, err)
	}
}

// What it costs when this fails: a file that grows past its limit between the check and the read
// is read cut short or whole, and the caller takes a part, or more than it planned for.
func TestReadOwnedRefusesAFileThatGrowsPastItsLimitWhileItIsRead(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	me := "uid:" + strconv.Itoa(os.Geteuid())
	p := filepath.Join(d, "f")
	if err := os.WriteFile(p, []byte("01234"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := local()
	h.stat = func(f *os.File) (node, error) {
		n, err := statFile(f)
		if err := os.WriteFile(p, []byte("012345"), 0o600); err != nil {
			t.Error(err)
		}
		return n, err
	}
	b, err := readOwned(p, me, true, 5, h, func() {})
	if u, ok := errors.AsType[*Unsafe](err); !ok || !errors.Is(err, ErrTooLong) || !strings.Contains(u.Observed, "grew past 5 bytes") || b != nil {
		t.Errorf("%q, %v, and want it refused as grown", b, err)
	}
}

// What it costs when this fails: a file swapped in at the name after the open is read unchecked,
// or checked in place of the one read, and another account plants what the caller reads, or a
// refused file passes by a good one swapped in behind it.
func TestReadOwnedReadsOnlyTheFileItCheckedWhenOneIsPlantedAfterTheOpen(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	me := "uid:" + strconv.Itoa(os.Geteuid())
	good, bad, p := filepath.Join(d, "good"), filepath.Join(d, "bad"), filepath.Join(d, "f")
	plant := func(from string) func() {
		planted := false
		return func() {
			if planted {
				return
			}
			planted = true
			if err := os.Rename(from, p); err != nil {
				t.Error(err)
			}
		}
	}
	write := func(name, body string, perm fs.FileMode) {
		if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(name, perm); err != nil {
			t.Fatal(err)
		}
	}
	write(p, "opened", 0o600)
	write(bad, "planted", 0o666)
	// On macOS the opened file's access list is read by its name, and the name now names another
	// file, so the name is opened again and the planted file judged whole, as a second call would
	// judge it: either way, the file read is the file judged.
	b, err := readOwned(p, me, true, 100, local(), plant(bad))
	switch {
	case listByName && (!errors.Is(err, ErrOthersWrite) || errors.Is(err, errors.ErrUnsupported) || b != nil):
		t.Errorf("a file others can write planted after the open: %q, %v, and want it opened again and refused by its mode", b, err)
	case !listByName && (err != nil || string(b) != "opened"):
		t.Errorf("a file others can write planted after the open: %q, %v, and want the file opened", b, err)
	}
	write(p, "opened", 0o666)
	write(good, "planted", 0o600)
	b, err = readOwned(p, me, true, 100, local(), plant(good))
	switch {
	case listByName && (err != nil || string(b) != "planted"):
		t.Errorf("a good file planted over one others can write: %q, %v, and want it opened again and read", b, err)
	case !listByName && (!errors.Is(err, ErrOthersWrite) || b != nil):
		t.Errorf("a good file planted over one others can write: %q, %v, and want ErrOthersWrite", b, err)
	}
}

// Account is "uid:" and this account's effective id, the owner of a file this account makes, and
// ReadOwned reads that file as Account's.
//
// What it costs when this fails: a program reading its own settings through ReadOwned as Account
// is refused its own file.
func TestAccountOwnsAFileThisAccountMadeOnThisDisk(t *testing.T) {
	t.Parallel()
	p := filepath.Join(realDir(t), "f")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Account()
	if want := "uid:" + strconv.Itoa(os.Geteuid()); err != nil || got != want {
		t.Fatalf("Account: %q, %v, and want %q", got, err, want)
	}
	if b, err := ReadOwned(p, got, true, 1); err != nil || string(b) != "x" {
		t.Errorf("ReadOwned as %s of a file it made: %q, %v", got, b, err)
	}
}

// What it costs when this fails: a path in the work tree on this disk passes because it was
// spelled through a link, or as the tree itself, and a head commits a file the tools read; or one
// beside the tree is refused.
func TestAPathInAnApartFolderOnThisDiskIsRefusedHoweverItIsSpelled(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	w, o := filepath.Join(d, "work"), filepath.Join(d, "other")
	for _, p := range []string{w, filepath.Join(w, "sub"), o} {
		mk(t, p, Folder, 0o755)
	}
	mk(t, filepath.Join(w, "sub", "f"), File, 0o644)
	mk(t, filepath.Join(o, "f"), File, 0o644)
	lw := filepath.Join(d, "lw")
	if err := os.Symlink("work", lw); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		path, apart string
		k           Kind
	}{
		{w, w, Folder},
		{filepath.Join(w, "sub", "f"), w, File},
		{filepath.Join(lw, "sub", "f"), w, File},
		{filepath.Join(w, "sub", "f"), lw, File},
	} {
		_, err := Path(c.path, c.k, Who{Apart: []string{c.apart}})
		wantRefused(t, c.path+" apart from "+c.apart, err, refusal{ErrInside, Fix{}, w + " is " + c.apart + ", a folder named to be kept apart"})
	}
	// In another case the tree is the same folder where the volume ignores case, as macOS's does by
	// default, and is refused; where the volume keeps case it is no name at all. It never passes.
	up := filepath.Join(d, "WORK", "sub", "f")
	_, err := Path(up, File, Who{Apart: []string{w}})
	if _, serr := os.Stat(up); serr == nil {
		wantRefused(t, up+", on a volume that ignores case", err, refusal{ErrInside, Fix{}, filepath.Join(d, "WORK") + " is " + w})
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s, on a volume that keeps case: %v, and want fs.ErrNotExist", up, err)
	}
	if got, err := Path(filepath.Join(o, "f"), File, Who{Apart: []string{w}}); err != nil || got != filepath.Join(o, "f") {
		t.Errorf("a file beside the apart folder: %q, %v, and want a pass", got, err)
	}
}

// OwnerOf reads the folder it was handed open, never the name: a link to / swapped in at the name
// after the open, the folder moved aside, does not lend the owner of /.
//
// What it costs when this fails: a folder swapped after the open is the one judged, and a program
// trusts root's folder's owner for one an attacker holds.
func TestOwnerOfReadsTheHandleAndNeverTheName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	if err := os.Mkdir(a, 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(a)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := os.Rename(a, filepath.Join(dir, "b")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/", a); err != nil {
		t.Fatal(err)
	}
	got, err := OwnerOf(f)
	want := "uid:" + strconv.Itoa(os.Geteuid())
	if err != nil || got != want {
		t.Errorf("OwnerOf of a folder moved aside for a link to /: %q (%v), and want %q, the folder's", got, err, want)
	}
	if os.Geteuid() == 0 {
		t.Log("root owns / too, so a read of the name would pass here")
	}
}

// What it costs when this fails: on this disk, a refusal of a folder on the way reads as one of
// the file asked for, and a caller tells a person to fix the wrong name.
func TestOnThisDiskUnsafePathIsTheFileOrTheFolderOnTheWayThatWasRefused(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	open, shut := filepath.Join(d, "open"), filepath.Join(d, "shut")
	mk(t, open, Folder, 0o777)
	mk(t, shut, Folder, 0o755)
	below, writable := filepath.Join(open, "f"), filepath.Join(shut, "f")
	mk(t, below, File, 0o644)
	mk(t, writable, File, 0o666)
	_, err := Path(below, File, Who{})
	wantPath(t, "a file below a folder others write", err, open)
	_, err = Path(writable, File, Who{})
	wantPath(t, "a file others write", err, writable)
	_, err = ReadOwned(writable, "uid:"+strconv.Itoa(os.Geteuid()), false, 10)
	wantPath(t, "ReadOwned of a file others write", err, writable)
}

// On this disk, a link to d/../x, with d a link to another folder, answers the x beside d's
// target, the file the system opens, not the x beside the link.
//
// What it costs when this fails: trust answers, and judges, a file other than the one a program
// opening the path reads.
func TestALinkWithADotDotAfterALinkOnThisDiskAnswersWhatTheSystemOpens(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	other := filepath.Join(d, "other")
	mk(t, filepath.Join(d, "x"), File, 0o644)
	mk(t, other, Folder, 0o755)
	mk(t, filepath.Join(other, "c"), Folder, 0o755)
	mk(t, filepath.Join(other, "x"), File, 0o644)
	if err := os.Symlink(filepath.Join(other, "c"), filepath.Join(d, "d")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("d"+string(filepath.Separator)+".."+string(filepath.Separator)+"x", filepath.Join(d, "l")); err != nil {
		t.Fatal(err)
	}
	l := filepath.Join(d, "l")
	opened, err := os.Stat(l)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Path(l, File, Who{})
	if err != nil || got != filepath.Join(other, "x") {
		t.Fatalf("Path(%s) = %q, %v, and want %q", l, got, err, filepath.Join(other, "x"))
	}
	if answered, err := os.Stat(got); err != nil || !os.SameFile(opened, answered) {
		t.Errorf("the answer %s is not the file the system opens at %s: %v", got, l, err)
	}
}

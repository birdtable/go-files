//go:build !windows

package write

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// ownerOf is the uid and gid of what is at path, not followed.
func ownerOf(t *testing.T, path string) (int, int) {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("%s's stat says no owner", path)
	}
	return int(st.Uid), int(st.Gid)
}

// otherGroup is a group this account may give a file in dir to that a new file there does not
// take on its own (the folder's on BSD and macOS, the account's own on Linux), so a given group is
// seen. It is the folder's group when there is none, and the test says so in its log.
func otherGroup(t *testing.T, dir string) int {
	t.Helper()
	_, folder := ownerOf(t, dir)
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if g != folder && g != os.Getegid() {
			return g
		}
	}
	t.Logf("this account is in no group besides %d and its folder's %d, so a given group is not seen here", os.Getegid(), folder)
	return folder
}

// tempIn is the one temp file a write has made in dir.
func tempIn(t *testing.T, dir, name string) string {
	t.Helper()
	found, err := filepath.Glob(filepath.Join(dir, "."+name+".*.tmp"))
	if err != nil || len(found) != 1 {
		t.Fatalf("the temp files of %s are %v (%v), and want one", name, found, err)
	}
	return found[0]
}

// The mode is exactly the one named, whatever the umask; with none it is 0600; Shared is 0644, and
// 0755 with Runnable.
//
// What it costs when this fails: a umask of 077 makes a shared file unreadable to the group it
// is for, or a umask of 0 makes a secret readable to every account, or a Shared file, a service's
// config other accounts read, is 0600 under one umask and 0666 under another.
func TestTheModeIsExactlyTheOneNamedUnderAnyUmask(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cases := []struct {
		mask, perm string
		want       os.FileMode
	}{
		{"077", "644", 0o644},
		{"077", "660", 0o660},
		{"0", "640", 0o640},
		{"0", "-", 0o600},
		{"077", "-", 0o600},
		{"077", "shared", 0o644},
		{"0", "shared", 0o644},
		{"077", "shared-run", 0o755},
		{"0", "shared-run", 0o755},
	}
	// Each case is a child process, and under -race each child sleeps a second at its exit, so the
	// cases run at once. A cleanup runs once every case has ended, and counts them.
	var ran atomic.Int64
	t.Cleanup(func() {
		if ran := ran.Load(); ran != int64(len(cases)) {
			t.Errorf("ran %d of %d cases", ran, len(cases))
		}
	})
	for i, c := range cases {
		t.Run(c.mask+"-"+c.perm, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(dir, "f"+string(rune('0'+i)))
			cmd := helperCmd(t, "umask", c.mask, path, c.perm)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("umask %s, mode %s: %v %s", c.mask, c.perm, err, out)
			}
			if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != c.want {
				t.Errorf("umask %s, mode %s: the file is %v (%v), and want %v", c.mask, c.perm, fi, err, c.want)
			}
			ran.Add(1)
		})
	}
}

// A log Append makes has exactly LogMode's mode, whatever the umask, at first and again after its
// move aside; with none it is 0600. A log already there keeps its own mode.
//
// What it costs when this fails: a service's group can read its log until the first move and
// never after, or a umask of 0 makes an audit log readable to every account.
func TestALogHasLogModeUnderAnyUmaskPastItsMove(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cases := []struct {
		mask, perm string
		want       os.FileMode
	}{
		{"077", "640", 0o640},
		{"0", "640", 0o640},
		{"077", "660", 0o660},
		{"0", "-", 0o600},
		{"077", "-", 0o600},
	}
	// Each case is a child process, and under -race each child sleeps a second at its exit, so the
	// cases run at once. A cleanup runs once every case has ended, and counts them.
	var ran atomic.Int64
	t.Cleanup(func() {
		if ran := ran.Load(); ran != int64(len(cases)) {
			t.Errorf("ran %d of %d cases", ran, len(cases))
		}
	})
	for i, c := range cases {
		t.Run(c.mask+"-"+c.perm, func(t *testing.T) {
			t.Parallel()
			log := filepath.Join(dir, "a"+string(rune('0'+i))+".log")
			cmd := helperCmd(t, "umask-log", c.mask, log, c.perm)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("umask %s, mode %s: %v %s", c.mask, c.perm, err, out)
			}
			holds(t, log+".1", "first\n")
			holds(t, log, "second\n")
			permIs(t, log+".1", c.want)
			permIs(t, log, c.want)
			ran.Add(1)
		})
	}

	theirs := filepath.Join(dir, "theirs.log")
	put0(t, theirs, "")
	if err := os.Chmod(theirs, 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	if err := Append(t.Context(), root, "theirs.log", []byte("line"), 100, Aside("theirs.log.1"), LogMode(0o600)); err != nil {
		t.Fatal(err)
	}
	holds(t, theirs, "line\n")
	permIs(t, theirs, 0o644)
}

// KeepMode keeps the mode of the plain file it replaces, and takes Mode's, or 0600, where there is
// none or a link is there: a link's target lends it nothing.
//
// What it costs when this fails: a config a person made 0640 for a service's group is made 0600,
// and the service cannot read it; or a link lends the file its target's mode.
func TestKeepModeKeepsTheModeOfTheFileItReplaces(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := File(path, []byte("new"), KeepMode(), Mode(0o600)); err != nil {
		t.Fatal(err)
	}
	permIs(t, path, 0o640)
	if err := File(filepath.Join(dir, "fresh"), []byte("new"), KeepMode(), Mode(0o644)); err != nil {
		t.Fatal(err)
	}
	permIs(t, filepath.Join(dir, "fresh"), 0o644)
	target := filepath.Join(dir, "target")
	put0(t, target, "theirs")
	if err := os.Chmod(target, 0o666); err != nil {
		t.Fatal(err)
	}
	plantLink(t, "target", filepath.Join(dir, "linked"))
	if err := File(filepath.Join(dir, "linked"), []byte("new"), KeepMode()); err != nil {
		t.Fatal(err)
	}
	permIs(t, filepath.Join(dir, "linked"), 0o600)
	permIs(t, target, 0o666)
}

// Owner's group is on the temp file before it is put in place, given on its descriptor.
//
// What it costs when this fails: a file is there as its writer's for a moment, and a service of
// that group cannot read it, or the owner is given by a name a link was swapped in at.
func TestTheOwnerIsGivenBeforeTheFileIsInPlace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	gid := otherGroup(t, dir)
	seen := -1
	err := File(path, []byte("new"), Owner(-1, gid), stepWith(func(at string) {
		if at == "written" {
			_, seen = ownerOf(t, tempIn(t, dir, "app.toml"))
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if seen != gid {
		t.Errorf("before the rename the temp file's group is %d, and want %d", seen, gid)
	}
	if uid, g := ownerOf(t, path); uid != os.Geteuid() || g != gid {
		t.Errorf("the file is %d:%d, and want %d:%d", uid, g, os.Geteuid(), gid)
	}
}

// A link swapped in at the temp name while the file is written is not put in place, and what it
// leads to keeps its bytes, mode and owner: the mode and owner go on the descriptor, and the name
// is checked to be the file written before the rename.
//
// What it costs when this fails: whoever can write the folder plants a link at the temp name, and
// a privileged writer chmods a file of their choosing, or puts their link at the config's name.
func TestALinkPlantedAtTheTempNameIsNotPutInPlace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	target := filepath.Join(dir, "target")
	put0(t, target, "theirs")
	uid, gid := ownerOf(t, target)
	err := File(path, []byte("new"), Mode(0o644), Owner(-1, otherGroup(t, dir)), stepWith(func(at string) {
		if at == "temp" {
			tmp := tempIn(t, dir, "app.toml")
			if err := os.Remove(tmp); err != nil {
				t.Fatal(err)
			}
			plantLink(t, "target", tmp)
		}
	}))
	if err == nil || !strings.Contains(err.Error(), "swapped") {
		t.Errorf("a link planted at the temp name: %v, and want it refused", err)
	}
	holds(t, path, "old")
	holds(t, target, "theirs")
	permIs(t, target, 0o600)
	if u, g := ownerOf(t, target); u != uid || g != gid {
		t.Errorf("the target is %d:%d, and was %d:%d", u, g, uid, gid)
	}
	alone(t, dir, "app.toml", "target")
}

// A pipe swapped in at the log between the look and the open, a link put over it to a file inside
// the root, which os.Root follows, or a link planted at a missing log's name before its exclusive
// create, is refused, and no line is written to any of them; a link with
// no target makes none. Another plain file of one name swapped in is a move: it is looked at again,
// and as it passes the look it gets the line, as any file a fresh look finds there would.
//
// What it costs when this fails: a swap timed to the look has a privileged writer append to a file
// of the swapper's choosing.
func TestAFileSwappedIntoTheLogAfterTheLookGetsNoLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	target := filepath.Join(dir, "target")
	put0(t, target, "theirs")
	swaps := map[string]func(path string) error{
		"pipe":     func(path string) error { return errors.Join(os.Remove(path), syscall.Mkfifo(path, 0o600)) },
		"link":     func(path string) error { return os.Symlink("target", path) },
		"dangling": func(path string) error { return os.Symlink("made", path) },
		"relink":   func(path string) error { return errors.Join(os.Remove(path), os.Symlink("target", path)) },
	}
	put0(t, filepath.Join(dir, "pipe"), "")
	put0(t, filepath.Join(dir, "relink"), "")
	ran := 0
	for kind, swap := range swaps {
		done := false
		err := appendWith(t.Context(), root, kind, []byte("line"), 100, settle([]Option{asideWith(kind + ".1"), stepWith(func(at string) {
			if at == "look" && !done {
				done = true
				if err := swap(filepath.Join(dir, kind)); err != nil {
					t.Fatal(err)
				}
			}
		})}))
		if err == nil {
			t.Errorf("a %s swapped in after the look got the line", kind)
		}
		ran++
	}
	if ran != len(swaps) {
		t.Fatalf("ran %d of %d swaps", ran, len(swaps))
	}
	holds(t, target, "theirs")
	if _, err := os.Lstat(filepath.Join(dir, "made")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a dangling link's target was made: %v", err)
	}

	put0(t, filepath.Join(dir, "file"), "")
	put0(t, filepath.Join(dir, "swapped"), "swapped")
	done := false
	err = appendWith(t.Context(), root, "file", []byte("line"), 100, settle([]Option{asideWith("file.1"), stepWith(func(at string) {
		if at == "look" && !done {
			done = true
			if err := os.Rename(filepath.Join(dir, "swapped"), filepath.Join(dir, "file")); err != nil {
				t.Fatal(err)
			}
		}
	})}))
	if err != nil || !done {
		t.Errorf("a plain file swapped in after the look (swapped %v): %v, and want it looked at again", done, err)
	}
	holds(t, filepath.Join(dir, "file"), "swapped\nline\n") // its torn last line ended first
}

// mkfifo makes a pipe at path, and a second pipe path+".held" that a writer holds open for the
// test, so a read of either would wait.
func mkfifo(t *testing.T, path string) {
	t.Helper()
	for _, p := range []string{path, path + ".held"} {
		if err := syscall.Mkfifo(p, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	w, err := os.OpenFile(path+".held", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
}

// A pipe where a plain file is read (IfHolds's file, Keep's file, PutBack's copy, Append's log) is
// refused at once, with no writer and with one that holds it, and not waited on. The look refuses
// it, before any open: the step "look", past the look, is never reached.
//
// What it costs when this fails: whoever can make a name in the folder hangs every write, copy,
// restore or log line of the program, or has it open a pipe or a device it was never to open.
func TestAPipeIsRefusedAndNotWaitedOn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "p")
	mkfifo(t, path)
	plain := filepath.Join(dir, "plain")
	put0(t, plain, "x")
	sum := sha256.Sum256([]byte("x"))
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ran := 0
	for _, p := range []string{path, path + ".held"} {
		calls := map[string]func(seen Option) error{
			"IfHolds read": func(seen Option) error { return File(p, []byte("new"), IfHolds(sum), seen) },
			"Keep copied": func(seen Option) error {
				_, err := Keep(p, dir, "app.toml.kept", seen)
				return err
			},
			"PutBack restored from": func(seen Option) error { return PutBack(p, plain, sum, seen) },
			"Append wrote to": func(seen Option) error {
				return appendWith(t.Context(), root, filepath.Base(p), []byte("line"), 100, settle([]Option{asideWith(filepath.Base(p) + ".1"), seen}))
			},
		}
		for what, call := range calls {
			looked := false
			if err := call(stepWith(func(at string) { looked = looked || at == "look" })); err == nil {
				t.Errorf("%s the pipe %s", what, p)
			}
			if looked {
				t.Errorf("%s: the pipe %s passed the look, and only the open's own stat refused it", what, p)
			}
			ran++
		}
	}
	if ran != 8 {
		t.Fatalf("ran %d of 8 calls", ran)
	}
	holds(t, plain, "x")
}

// A pipe or a link swapped in between the look at a plain file and its open is refused by the
// open's own stat, at once: the open does not wait for a writer, and reads only the file the look
// saw. What each holds hashes to the digest given, so only the refusal stops the write.
//
// What it costs when this fails: a swap timed to the look hangs the write that hashes the file,
// or has it hash a file of the swapper's choosing.
func TestAFileSwappedInAfterTheLookIsNotRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	put0(t, target, "")
	swaps := map[string]func(path string) error{
		"pipe": func(path string) error { return syscall.Mkfifo(path, 0o600) },
		"link": func(path string) error { return os.Symlink("target", path) },
	}
	ran := 0
	for kind, swap := range swaps {
		path := filepath.Join(dir, kind)
		put0(t, path, "")
		err := File(path, []byte("new"), IfHolds(sha256.Sum256(nil)), stepWith(func(at string) {
			if at == "look" {
				if err := errors.Join(os.Remove(path), swap(path)); err != nil {
					t.Fatal(err)
				}
			}
		}))
		if err == nil {
			t.Errorf("a %s swapped in after the look was read, and the write went over it", kind)
		}
		if fi, err := os.Lstat(path); err != nil || fi.Mode().IsRegular() {
			t.Errorf("the %s is %v (%v), and want it left", kind, fi, err)
		}
		ran++
	}
	if ran != len(swaps) {
		t.Fatalf("ran %d of %d swaps", ran, len(swaps))
	}
	holds(t, target, "")
}

// PutBack hashes and replaces the file through the one folder it opened: a folder swapped in at
// the file's path after the hash is not the one restored into.
//
// What it costs when this fails: a restore checked against one folder writes into another a
// person swapped in, over a file it never checked.
func TestPutBackRestoresIntoTheFolderItHashedIn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keep := filepath.Join(dir, "kept")
	conf := filepath.Join(dir, "conf")
	for _, d := range []string{keep, conf} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(conf, "app.toml")
	put0(t, path, "before")
	kept, err := Keep(path, keep, "app.toml.kept")
	if err != nil {
		t.Fatal(err)
	}
	put0(t, path, "after")
	looks := 0
	err = PutBack(kept, path, sha256.Sum256([]byte("after")), stepWith(func(at string) {
		if at != "look" {
			return
		}
		if looks++; looks == 2 { // the first is the copy's, the second the file's
			if err := os.Rename(conf, conf+".moved"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(conf, 0o700); err != nil {
				t.Fatal(err)
			}
			put0(t, path, "after")
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if looks != 2 {
		t.Fatalf("PutBack looked %d times, and want 2", looks)
	}
	holds(t, path, "after")
	holds(t, filepath.Join(conf+".moved", "app.toml"), "before")
}

// Keep gives the copy Owner's group, and PutBack gives it to the restored file, with the copy's
// bytes and mode.
//
// What it costs when this fails: an undo of a service's config gives it back to the account that
// ran the undo, and the service cannot read it.
func TestKeepAndPutBackGiveTheOwner(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "before")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	gid := otherGroup(t, dir)
	kept, err := Keep(path, dir, "app.toml.kept", Owner(-1, gid))
	if err != nil {
		t.Fatal(err)
	}
	if _, g := ownerOf(t, kept); g != gid {
		t.Errorf("the copy's group is %d, and want %d", g, gid)
	}
	if err := File(path, []byte("after")); err != nil {
		t.Fatal(err)
	}
	if err := PutBack(kept, path, sha256.Sum256([]byte("after")), Owner(-1, gid)); err != nil {
		t.Fatal(err)
	}
	holds(t, path, "before")
	permIs(t, path, 0o640)
	if _, g := ownerOf(t, path); g != gid {
		t.Errorf("the restored file's group is %d, and want %d", g, gid)
	}
}

// KeepOwner gives the new file the group of the plain file it replaces, on the temp file before
// the rename, over Owner's; a missing file gives Owner's; a link at the name lends nothing of its
// target's, which keeps its own.
//
// What it costs when this fails: a shared repository's file, its group's to read and write, is
// replaced as its writer's group, and the others of the group are locked out of it; or a link
// planted at the name chooses the group a privileged writer gives the file.
func TestKeepOwnerKeepsTheGroupOfTheFileItReplaces(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	gid := otherGroup(t, dir)
	if err := os.Chown(path, -1, gid); err != nil {
		t.Fatal(err)
	}
	seen := -1
	err := File(path, []byte("new"), KeepOwner(), Owner(-1, os.Getegid()), stepWith(func(at string) {
		if at == "written" {
			_, seen = ownerOf(t, tempIn(t, dir, "app.toml"))
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if seen != gid {
		t.Errorf("before the rename the temp file's group is %d, and want %d, the replaced file's", seen, gid)
	}
	if uid, g := ownerOf(t, path); uid != os.Geteuid() || g != gid {
		t.Errorf("the file is %d:%d, and want %d:%d, the replaced file's", uid, g, os.Geteuid(), gid)
	}
	fresh := filepath.Join(dir, "fresh")
	if err := File(fresh, []byte("new"), KeepOwner(), Owner(-1, gid)); err != nil {
		t.Fatal(err)
	}
	if _, g := ownerOf(t, fresh); g != gid {
		t.Errorf("a file with none to replace is of group %d, and want Owner's %d", g, gid)
	}
	target := filepath.Join(dir, "target")
	put0(t, target, "theirs")
	if err := os.Chown(target, -1, gid); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "plain")
	put0(t, plain, "x")
	_, made := ownerOf(t, plain)
	plantLink(t, "target", filepath.Join(dir, "linked"))
	if err := File(filepath.Join(dir, "linked"), []byte("new"), KeepOwner()); err != nil {
		t.Fatal(err)
	}
	if _, g := ownerOf(t, filepath.Join(dir, "linked")); g != made {
		t.Errorf("a file put over a link is of group %d, its target's, and want %d, a new file's", g, made)
	}
	holds(t, target, "theirs")
	if _, g := ownerOf(t, target); g != gid {
		t.Errorf("the link's target is now of group %d, and was %d", g, gid)
	}
}

// A group this account may not give a file to, under Owner or the group KeepOwner keeps, is
// refused with an error matching fs.ErrPermission, and nothing is replaced or left behind. Root
// may give any group, and is told so in the log.
//
// What it costs when this fails: a file is replaced as its writer's group while the program
// believes it kept the group, or a temp file is left on every refused write.
func TestAGroupThisAccountMayNotGiveIsRefusedAndReplacesNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	notMine := 4242
	for slices.Contains(groups, notMine) || notMine == os.Getegid() {
		notMine++
	}
	err = File(path, []byte("new"), Owner(-1, notMine))
	if os.Geteuid() == 0 {
		t.Logf("root may give group %d: %v", notMine, err)
		return
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("Owner(-1, %d), a group not this account's: %v, and want fs.ErrPermission", notMine, err)
	}
	holds(t, path, "old")
	alone(t, dir, "app.toml")
}

// SyncFolder of a pipe at dir is refused at once as not a folder, and never waits for a writer.
//
// What it costs when this fails: an account that may make names beneath the root puts a pipe
// where a folder was, and the program that flushes it after a removal hangs for as long as that
// account likes.
func TestSyncFolderRefusesAPipeAtOnce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "d"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	done := make(chan error, 1)
	go func() { done <- SyncFolder(root, "d") }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "is not a folder") {
			t.Errorf("SyncFolder of a pipe: %v, and want it refused as not a folder", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("SyncFolder of a pipe is still waiting after ten seconds")
		if w, err := os.OpenFile(filepath.Join(dir, "d"), os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			defer w.Close()
		}
		<-done
	}
}

// A temp name removed and made again after the write's close is refused: the write holds a
// descriptor of its temp file till its last check, so the file made there cannot have its inode number,
// which Linux gives to the next file made once it is freed. It is red, with the hold
// cut, only where the file system gives a freed inode number to the next file made, as ext4 does;
// tmpfs, btrfs and APFS count upward, so there it passes either way.
//
// What it costs when this fails: whoever can write the folder removes the temp file and makes one
// of their own at its name, which takes the freed number and is put in place, unflushed.
func TestATempNameRemadeAfterTheCloseIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	err := File(path, []byte("new"), stepWith(func(at string) {
		if at != "closed" {
			return
		}
		tmp, err := filepath.Glob(filepath.Join(dir, ".app.toml.*.tmp"))
		if err != nil || len(tmp) != 1 {
			t.Fatalf("the temp files %v, %v, and want one", tmp, err)
		}
		if err := os.Remove(tmp[0]); err != nil {
			t.Fatal(err)
		}
		put0(t, tmp[0], "theirs")
	}))
	if err == nil || !strings.Contains(err.Error(), "swapped") {
		t.Errorf("a temp name remade after the close: %v, and want it refused as swapped", err)
	}
	holds(t, path, "old")
	alone(t, dir, "app.toml")
}

// A file put at the temp name before the write opens it again to read is refused there: the
// descriptor held till the last check is the file made, never the one put there.
//
// What it costs when this fails: whoever can write the folder renames a file of their own over the
// temp name before the write's reopen, the write holds their file, compares the name with it, and
// puts their file in place.
func TestAFileRenamedOverTheTempNameBeforeItsReopenIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	theirs := filepath.Join(dir, "theirs")
	put0(t, theirs, "theirs")
	err := File(path, []byte("new"), stepWith(func(at string) {
		if at != "temp" {
			return
		}
		tmp, err := filepath.Glob(filepath.Join(dir, ".app.toml.*.tmp"))
		if err != nil || len(tmp) != 1 {
			t.Fatalf("the temp files %v, %v, and want one", tmp, err)
		}
		if err := os.Rename(theirs, tmp[0]); err != nil {
			t.Fatal(err)
		}
	}))
	if err == nil || !strings.Contains(err.Error(), "swapped") {
		t.Errorf("a file renamed over the temp name before its reopen: %v, and want it refused as swapped", err)
	}
	holds(t, path, "old")
	alone(t, dir, "app.toml")
}

// A mode the writer may not read by writes as it did before the read descriptor: that descriptor
// is opened while the temp file is still 0600 and the writer's.
//
// What it costs when this fails: a write-only file, or one that is only run, can no longer be put
// in place by write at all.
func TestAModeTheWriterMayNotReadByStillWrites(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		opts []Option
		perm os.FileMode
	}{
		{"0200", []Option{Mode(0o200)}, 0o200},
		{"0000", []Option{Mode(0)}, 0},
		{"Runnable 0111", []Option{Runnable(), Mode(0o111)}, 0o111},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "app.toml")
			if err := File(path, []byte("new"), tc.opts...); err != nil {
				t.Fatalf("Mode %v: %v, and want it written", tc.perm, err)
			}
			permIs(t, path, tc.perm)
			if fi, err := os.Stat(path); err != nil || fi.Size() != 3 {
				t.Errorf("the file put in place: %v, %v, and want its 3 bytes", fi, err)
			}
			alone(t, dir, "app.toml")
		})
	}
}

// A file swapped in at the temp name after the last check is not placed under NoReplace: the link
// goes by the temp name, so the placed name is read against the file written, and a file that is
// not it is taken away again, with the write refused as swapped. The temp name is left clean, and
// the name stays missing.
//
// What it costs when this fails: whoever can write the folder puts a file of their own at the name
// a key or a lock is to be made at, in the one step the checks before it did not cover.
func TestAFileSwappedInAtTheTempNameAfterTheLastCheckIsNotPlacedByNoReplace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.key")
	err := File(path, []byte("mine"), NoReplace(), stepWith(func(at string) {
		if at != "written" {
			return
		}
		tmp := tempIn(t, dir, "app.key")
		if err := os.Remove(tmp); err != nil {
			t.Fatal(err)
		}
		put0(t, tmp, "theirs")
	}))
	if err == nil || !strings.Contains(err.Error(), "swapped") {
		t.Errorf("a file swapped in at the temp name after the last check: %v, and want it refused as swapped", err)
	}
	alone(t, dir)
}

// ready hands back a grip that holds the file written, whatever the file system: the grip's
// descriptor is the file's own, so its stat is the stat ready took, and no other file can have its
// inode number while it is open. A file system that gives a freed number to the next file made
// (the ext family, which this host's temp folder is on by statfs) shows the loss of it by the row at the name;
// tmpfs and APFS do not, so this row reads the grip itself.
//
// What it costs when this fails: nothing holds the file written to the link on the file systems
// that never reuse a number, and on those that do, the compare of the placed name can be met by
// another file.
func TestTheGripReadyGivesIsTheFileWritten(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	o := settle(nil)
	_, held, err := create(root, ".app.toml.0.tmp", bytes.NewReader([]byte("x")), 0o600, o)
	if err != nil {
		t.Fatal(err)
	}
	mine, g, err := ready(root, ".app.toml.0.tmp", held, o)
	if err != nil {
		t.Fatal(err)
	}
	if g == nil || g.file == nil {
		t.Fatal("ready gave no grip, and the file written is not held after it")
	}
	defer g.close()
	got, err := g.file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(mine, got) {
		t.Errorf("the grip is %v, and the file written is %v", got, mine)
	}
}

// At the taking step, between the two looks of a link's take-away, a name another process made
// since the link is left: the write is refused as swapped, and what is at the name is that
// process's file.
//
// What it costs when this fails: the take-away removes a name it did not put there.
func TestATakeAwayLeavesANameAnotherProcessMadeInTheGap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.key")
	err := File(path, []byte("mine"), NoReplace(), stepWith(func(at string) {
		switch at {
		case "written":
			tmp := tempIn(t, dir, "app.key")
			if err := os.Remove(tmp); err != nil {
				t.Fatal(err)
			}
			put0(t, tmp, "theirs")
		case "taking":
			other := filepath.Join(dir, "other")
			put0(t, other, "another's")
			if err := os.Rename(other, path); err != nil {
				t.Fatal(err)
			}
		}
	}))
	if err == nil || !strings.Contains(err.Error(), "swapped") {
		t.Errorf("a name made in the gap: %v, and want the write refused as swapped", err)
	}
	holds(t, path, "another's")
}

// A take-away that fails says the swapped file is still at the name: the error joins the refusal
// and names the name.
//
// What it costs when this fails: the write refuses as swapped and says nothing of the file that
// stays at the name, and a caller reads the name as free.
func TestATakeAwayThatFailsSaysTheSwappedFileIsInPlace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.key")
	denied := func(*os.Root, string) error { return fs.ErrPermission }
	err := File(path, []byte("mine"), NoReplace(), removeWith(denied), stepWith(func(at string) {
		if at != "written" {
			return
		}
		tmp := tempIn(t, dir, "app.key")
		if err := os.Remove(tmp); err != nil {
			t.Fatal(err)
		}
		put0(t, tmp, "theirs")
	}))
	if err == nil || !strings.Contains(err.Error(), "swapped") || !strings.Contains(err.Error(), "holds the swapped file, and its name could not be removed") {
		t.Errorf("a take-away that fails: %v, and want it refused as swapped, with the name said to hold the swapped file", err)
	}
}

// A link that fails, with a file swapped in at the temp name, is worded as swapped, and still
// matches what failed it: a taken name is fs.ErrExist.
//
// What it costs when this fails: the refusal of a swapped write reads as a plain link error, and a
// caller takes the temp file for the one it wrote.
func TestALinkThatFailsAfterASwapIsWordedAsSwapped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.key")
	put0(t, path, "taken")
	err := File(path, []byte("mine"), NoReplace(), stepWith(func(at string) {
		if at != "written" {
			return
		}
		tmp := tempIn(t, dir, "app.key")
		if err := os.Remove(tmp); err != nil {
			t.Fatal(err)
		}
		put0(t, tmp, "theirs")
	}))
	if !errors.Is(err, fs.ErrExist) || err == nil || !strings.Contains(err.Error(), "swapped") {
		t.Errorf("a taken name with a swapped temp file: %v, and want fs.ErrExist worded as swapped", err)
	}
	holds(t, path, "taken")
}

// Swap moves files by rename, which needs no read of their bytes, so a file the account may write
// but not read (mode 0200) still swaps: on Unix the no-index check opens nothing.
//
// What it costs when this fails: a program left only writable, or one a deploy set to 0200, can no
// longer be replaced by Swap, which opened it to read before it moved it.
func TestSwapSwapsAFileItMayNotRead(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"file", "fresh"} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			file, fresh, aside := filepath.Join(dir, "app"), filepath.Join(dir, "app.new"), filepath.Join(dir, "app.old")
			put0(t, file, "old")
			put0(t, fresh, "new")
			unreadable := file
			if in == "fresh" {
				unreadable = fresh
			}
			if err := os.Chmod(unreadable, 0o200); err != nil {
				t.Fatal(err)
			}
			if err := Swap(file, fresh, aside); err != nil {
				t.Fatalf("Swap with %s at mode 0200: %v, and want it to swap", in, err)
			}
			for _, p := range []string{file, aside} {
				if err := os.Chmod(p, 0o600); err != nil { // the 0200 name moved; read the bytes back
					t.Fatal(err)
				}
			}
			holds(t, file, "new")
			holds(t, aside, "old")
			alone(t, dir, "app", "app.old")
		})
	}
}

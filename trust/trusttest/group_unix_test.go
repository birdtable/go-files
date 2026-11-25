//go:build unix

package trusttest_test

import (
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"github.com/birdtable/go-files/trust/trusttest"
)

// groupOf is the group of p, followed where it is a link.
func groupOf(t *testing.T, p string) uint32 {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info.Sys().(*syscall.Stat_t).Gid
}

// modeOf is the mode bits of p that os.Chmod sets, the special ones among them.
func modeOf(t *testing.T, p string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode() & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)
}

// mine is this account's groups, its effective group first, each once.
func mine(t *testing.T) []uint32 {
	t.Helper()
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	var out []uint32
	for _, g := range append([]int{os.Getegid()}, groups...) {
		if g >= 0 && !slices.Contains(out, uint32(g)) {
			out = append(out, uint32(g))
		}
	}
	return out
}

// givable is those of mine a file of this account's in dir is given to by os.Chown, keeping the
// group. In a user namespace with no map for this account's other groups, getgroups names each as
// the overflow group, 65534, which chown refuses with EINVAL, so it is left out.
func givable(t *testing.T, dir string) []uint32 {
	t.Helper()
	probe := filepath.Join(dir, "probe")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var out []uint32
	for _, g := range mine(t) {
		if os.Chown(probe, -1, int(g)) == nil && groupOf(t, probe) == g {
			out = append(out, g)
		}
	}
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}
	return out
}

// What it costs when this fails: a test that plants a name of this account's given to a group, as
// the admin group, plants it in this account's own group and shows nothing, or leaves the name in
// that group, or with its set-user-ID and set-group-ID bits cleared, after the test.
func TestGiveGroupGivesTheNameToTheGroupUntilTheTestEnds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	groups := givable(t, dir)
	if len(groups) < 2 {
		// A skip fails the run (tests.admission.5), so the row logs and shows nothing here.
		t.Logf("this account may give a name to the one group %v of its groups %v, as in a user namespace that maps no other, and the row needs two, so it shows nothing", groups, mine(t))
		return
	}
	from, to := groups[0], groups[1]
	for _, c := range []struct {
		name string
		mode fs.FileMode
		link bool
	}{
		{"a file", 0o600, false},
		{"a file set-user-ID and set-group-ID", 0o750 | fs.ModeSetuid | fs.ModeSetgid, false},
		{"a folder", 0o700 | fs.ModeDir, false},
		{"a link to a file", 0o600, true},
	} {
		p := filepath.Join(dir, c.name)
		var err error
		if c.mode.IsDir() {
			err = os.Mkdir(p, 0o700)
		} else {
			err = os.WriteFile(p, []byte("data"), 0o600)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(p, -1, int(from)); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, c.mode&^fs.ModeDir); err != nil {
			t.Fatal(err)
		}
		mode := modeOf(t, p)
		if mode != c.mode&^fs.ModeDir {
			t.Fatalf("%s: its mode is %v, and want %v, before the change", c.name, mode, c.mode&^fs.ModeDir)
		}
		name := p
		if c.link {
			name = p + " link"
			if err := os.Symlink(p, name); err != nil {
				t.Fatal(err)
			}
		}
		r := call(t, func(r *recorder) { trusttest.GiveGroup(r, name, to) })
		if r.failed || r.skipped {
			t.Fatalf("%s: %q", c.name, r.said)
		}
		if got := groupOf(t, p); got != to {
			t.Errorf("%s: its group is %d, and want %d, given", c.name, got, to)
		}
		r.end()
		if got := groupOf(t, p); got != from || r.failed {
			t.Errorf("%s: its group is %d after, and was %d: %q", c.name, got, from, r.said)
		}
		if got := modeOf(t, p); got != mode {
			t.Errorf("%s: its mode is %v after, and was %v", c.name, got, mode)
		}
	}
}

// What it costs when this fails: a test that cannot give its name to a group passes having planted
// nothing, or fails for what is the system's or the account's, not the program's, or plants a name
// in no group at all, which chown reads math.MaxUint32 as, or a disk that keeps no groups passes it
// unchanged.
func TestGiveGroupSkipsWhereTheGroupCannotBeGivenAndFailsOtherwise(t *testing.T) {
	t.Parallel()
	p, _ := private(t, false)
	before := groupOf(t, p)
	refused := func(errno syscall.Errno) func(string, int, int) error {
		return func(name string, _, _ int) error {
			return &fs.PathError{Op: "chown", Path: name, Err: errno}
		}
	}
	called := false
	for _, c := range []struct {
		name  string
		gid   uint32
		chown func(string, int, int) error
		skip  bool
		says  string
	}{
		{"refused, EPERM", 1, refused(syscall.EPERM), true, "may not give it to the group 1"},
		{"refused, EINVAL", 1, refused(syscall.EINVAL), true, "not one this user namespace maps"},
		{"failing, ENOENT", 1, refused(syscall.ENOENT), false, "no such file"},
		{"kept by the disk", 1, func(string, int, int) error { return nil }, true, "its disk kept the group"},
		{"of no group", math.MaxUint32, func(string, int, int) error { called = true; return nil }, false, "is no group"},
	} {
		r := call(t, func(r *recorder) { trusttest.GiveGroupBy(r, p, c.gid, c.chown) })
		if r.skipped != c.skip || r.failed == c.skip || !r.says(c.says) {
			t.Errorf("%s: skipped %v, failed %v, %q, and want skipped %v saying %q", c.name, r.skipped, r.failed, r.said, c.skip, c.says)
		}
		if len(r.cleanups) != 0 {
			t.Errorf("%s: %d cleanups asked for, and want none", c.name, len(r.cleanups))
		}
	}
	if called {
		t.Error("of no group: chown was called")
	}
	if got := groupOf(t, p); got != before {
		t.Errorf("its group is %d, and was %d", got, before)
	}

	if os.Geteuid() == 0 {
		t.Log("as root, any group may be given, so no real refusal is shown")
		return
	}
	other := uint32(0)
	for slices.Contains(mine(t), other) {
		other++
	}
	r := call(t, func(r *recorder) { trusttest.GiveGroup(r, p, other) })
	if !r.skipped || r.failed {
		t.Errorf("a group %d this account is not in: skipped %v, failed %v, %q", other, r.skipped, r.failed, r.said)
	}
	if got := groupOf(t, p); got != before {
		t.Errorf("a group %d this account is not in: its group is %d, and was %d", other, got, before)
	}

	// A name whose own group is not this account's, as one below /private/tmp takes wheel on macOS,
	// is skipped before any chown, as it could not be given back.
	tmp, err := os.MkdirTemp("/private/tmp", "givegroup")
	if err != nil {
		t.Logf("no folder is made below /private/tmp, as off macOS, so the row of a name in another group shows nothing: %v", err)
		return
	}
	t.Cleanup(func() {
		if err := os.Remove(tmp); err != nil {
			t.Error(err)
		}
	})
	if g := groupOf(t, tmp); slices.Contains(mine(t), g) {
		t.Logf("%s is in the group %d, one of this account's, so the row of a name in another group shows nothing", tmp, g)
		return
	}
	called = false
	r = call(t, func(r *recorder) {
		trusttest.GiveGroupBy(r, tmp, mine(t)[0], func(string, int, int) error { called = true; return nil })
	})
	if !r.skipped || r.failed || !r.says("could not give it back") || called || len(r.cleanups) != 0 {
		t.Errorf("a name in the group %d, not this account's: skipped %v, failed %v, %q, chown called %v, %d cleanups, and want it skipped before any chown", groupOf(t, tmp), r.skipped, r.failed, r.said, called, len(r.cleanups))
	}
}

// What it costs when this fails: a group that cannot be put back stays given after the test, with
// nothing said, or a test that removed its name fails for the group it can no longer put back.
func TestGiveGroupFailsAPutBackRefusedButNotOfANameRemoved(t *testing.T) {
	t.Parallel()
	p, _ := private(t, false)
	gid := groupOf(t, p)
	calls := 0
	once := func(name string, uid, gid int) error {
		if calls++; calls > 1 {
			return &fs.PathError{Op: "chown", Path: name, Err: syscall.EPERM}
		}
		return os.Chown(name, uid, gid)
	}
	r := call(t, func(r *recorder) { trusttest.GiveGroupBy(r, p, gid, once) })
	if r.failed || r.skipped {
		t.Fatalf("%q", r.said)
	}
	if r.end(); !r.failed || !r.says("putting back what it changed") {
		t.Errorf("a put back refused: failed %v, %q", r.failed, r.said)
	}

	r = call(t, func(r *recorder) { trusttest.GiveGroup(r, p, gid) })
	if r.failed || r.skipped {
		t.Fatalf("%q", r.said)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if r.end(); r.failed {
		t.Errorf("a name the test removed: %q", r.said)
	}
}

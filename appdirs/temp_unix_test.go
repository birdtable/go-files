//go:build unix

package appdirs

import (
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/birdtable/go-files/trust"
)

// testDir is a folder of the test's own, read through its links, that the walk passes.
func testDir(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Fatal("run as root, which every rule trusts: the test tier runs as an account that is not root")
	}
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := trust.Path(d, trust.Folder, trust.Who{NoLinks: true}); err != nil {
		t.Fatalf("the test's own folder does not pass, so no case here says anything: %v", err)
	}
	return d
}

// link makes at a symbolic link to target.
func link(t *testing.T, at, target string) {
	t.Helper()
	if err := os.Symlink(target, at); err != nil {
		t.Fatal(err)
	}
}

// What it costs when this fails: a folder on the way that its group or others can write, where
// another account may add or swap the temp folder, holds the program's files.
func TestOnUnixAFolderOfTheWayItsGroupOrOthersCanWriteIsRefusedAndNothingIsMade(t *testing.T) {
	t.Parallel()
	b := testDir(t)
	for _, mode := range []os.FileMode{0o770, 0o777} {
		p := filepath.Join(b, mode.String())
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(p, "app")
		if got, err := temp(dir, nil, nil, false); !errors.Is(err, trust.ErrOthersWrite) {
			t.Errorf("temp below a folder of mode %v = %q, %v, want ErrOthersWrite", mode, got, err)
		}
		absent(t, dir)
	}
}

// What it costs when this fails: a temp folder this account made earlier, that its group or others
// may read, is answered, and another account reads what the program keeps there.
func TestOnUnixATempFolderAlreadyThereItsGroupOrOthersCanReadIsRefusedAndKeptAsItIs(t *testing.T) {
	t.Parallel()
	b := testDir(t)
	for _, mode := range []os.FileMode{0o700, 0o750, 0o705} {
		dir := filepath.Join(b, mode.String())
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		got, err := temp(dir, nil, nil, false)
		switch want := "has the mode 0" + strconv.FormatUint(uint64(mode), 8); {
		case mode == 0o700 && err != nil:
			t.Errorf("temp at a folder of mode %v: %v, and want it answered", mode, err)
		case mode != 0o700 && (!errors.Is(err, trust.ErrOthersRead) || !strings.Contains(err.Error(), want)):
			t.Errorf("temp at a folder of mode %v = %q, %v, and want ErrOthersRead saying %q", mode, got, err, want)
		}
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Errorf("temp changed the mode %v to %v", mode, info.Mode().Perm())
		}
	}
}

// What it costs when this fails: the temp folder is made so its group or others may read it, and
// another account reads what the program keeps there.
func TestOnUnixTheTempFolderIsMadeForThisAccountAlone(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(testDir(t), tempName("app"))
	if _, err := temp(dir, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("the temp folder is %v, want 0700", info.Mode())
	}
}

// sticky is root's sticky folder /tmp, read through its links, with a name of the test's own in it
// that is not there and is removed, with whatever was made at it, when the test ends.
func sticky(t *testing.T) string {
	t.Helper()
	tmp, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !ok || st.Uid != 0 || info.Mode()&fs.ModeSticky == 0 || info.Mode().Perm()&0o022 == 0 {
		t.Fatalf("%s is not root's sticky folder (%v), which this test needs", tmp, info.Mode())
	}
	p := filepath.Join(tmp, "go-files-appdirs-test-"+rand.Text())
	t.Cleanup(func() {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("removing %s: %v", p, err)
		}
	})
	return p
}

// What it costs when this fails: a link right in /tmp, where another account can make one, picks
// the folder the program's temp folder is made in.
func TestOnUnixATempFolderThroughALinkTrustRefusesIsRefusedAndNothingIsMade(t *testing.T) {
	t.Parallel()
	real := folder(t, filepath.Join(testDir(t), "real"))
	l := sticky(t)
	link(t, l, real)
	if base, err := tempBase(l, "app", trust.Who{}); !errors.Is(err, trust.ErrNotOwners) {
		t.Errorf("tempBase through a link right in root's sticky folder = %q, %v, want ErrNotOwners", base, err)
	}
	absent(t, filepath.Join(real, "app"))
}

// What it costs when this fails: a Who.Mounts entry that names nothing, read as trust's
// fs.ErrNotExist, is taken for the temp folder not yet made, and the walk under it is a pass.
func TestOnUnixAWhoMountsEntryThatNamesNothingRefusesTheTempBase(t *testing.T) {
	t.Parallel()
	gone := "/go-files-no-such-entry-" + rand.Text()
	if base, err := tempBase(testDir(t), "app", trust.Who{Mounts: []string{gone}}); !errors.Is(err, trust.ErrMountEntry) || errors.Is(err, fs.ErrNotExist) {
		t.Errorf("tempBase with Who.Mounts naming %s, which names nothing: %q, %v, and want ErrMountEntry, never fs.ErrNotExist", gone, base, err)
	}
}

// What it costs when this fails: a program has no temp folder of its own in /tmp, where Temp makes
// it with TMPDIR unset, as for root in a container, or its folder there is not
// private. It makes no use of testDir, so it runs as root too: go test -run with its name alone.
func TestOnUnixATempFolderRightInRootsStickyFolderIsThisAccountsOwn(t *testing.T) {
	t.Parallel()
	dir := sticky(t)
	for range 2 {
		if got, err := temp(dir, nil, nil, false); err != nil || got != dir {
			t.Fatalf("temp right in root's sticky folder as uid %d = %q, %v, want %s", os.Geteuid(), got, err, dir)
		}
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("the temp folder is %v, want 0700", info.Mode())
	}
}

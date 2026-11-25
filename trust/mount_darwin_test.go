package trust

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test makes and attaches a disk image with diskutil; the module has no runner
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// attach makes an 8 MB APFS disk image in d and mounts it on a new folder there, with owners off,
// as an external disk usually is, unless owners, and answers that folder. It is ejected when the
// test ends, before d is removed. diskutil is on every Mac, so nothing here skips: a Mac that
// cannot attach one fails the test. The disk every other test here writes on keeps ownership,
// and their passing is the owners-off image's other half.
func attach(t *testing.T, d string, owners bool) string {
	t.Helper()
	img, mnt := filepath.Join(d, "ext.dmg"), filepath.Join(d, "ext")
	if out, err := exec.CommandContext(t.Context(), "/usr/sbin/diskutil", "image", "create", "blank", "--fs", "APFS", "--size", "8m", "--volumeName", "ext", img).CombinedOutput(); err != nil { //nolint:gosec // G204: diskutil by its full path, on the test's own folder
		t.Fatalf("diskutil image create: %v: %s", err, out)
	}
	if err := os.Mkdir(mnt, 0o700); err != nil {
		t.Fatal(err)
	}
	args := []string{"image", "attach", "--mountPoint", mnt, "--mountOptions", "nobrowse", img}
	if args = append(args, "--mountOptions", "noowners"); owners {
		args[len(args)-1] = "owners" // an image this account attaches ignores ownership unless asked
	}
	if out, err := exec.CommandContext(t.Context(), "/usr/sbin/diskutil", args...).CombinedOutput(); err != nil { //nolint:gosec // G204: diskutil by its full path, on the test's own folder
		t.Fatalf("diskutil image attach: %v: %s", err, out)
	}
	t.Cleanup(func() { eject(t, mnt, ejects()) })
	var st unix.Statfs_t
	if err := unix.Statfs(mnt, &st); err != nil || (st.Flags&unix.MNT_IGNORE_OWNERSHIP == 0) != owners {
		t.Fatalf("the image on %s is mounted with the flags %#x, %v, and want MNT_IGNORE_OWNERSHIP set: %t", mnt, st.Flags, err, !owners)
	}
	return mnt
}

// ejects are the ways tried, in order, to take an image off its folder: diskutil's forced eject,
// three times, as it fails once in a while on a busy Mac and passes on the next try,
// and then hdiutil's forced detach, which goes to the image's device by another
// road. There is no wait between tries: each one is a round trip to the system's disk daemon.
func ejects() [][]string {
	return [][]string{
		{"/usr/sbin/diskutil", "eject", "force"},
		{"/usr/sbin/diskutil", "eject", "force"},
		{"/usr/sbin/diskutil", "eject", "force"},
		{"/usr/bin/hdiutil", "detach", "-force"},
	}
}

// eject takes the image off mnt by the first of ways that does, each given mnt last. Tries that
// failed before one passed are logged, not failed: the rule was judged before the cleanup, and a
// cleanup that got there is not the rule's result. When none passes, the test fails, and says it
// is the image's cleanup, not the rule, and that the folder is still mounted. A folder that is no
// longer mounted on is left as it is.
func eject(t testing.TB, mnt string, ways [][]string) {
	t.Helper()
	var failed []string
	for _, way := range ways {
		if !mounted(t, mnt) {
			return
		}
		out, err := exec.Command(way[0], append(way[1:len(way):len(way)], mnt)...).CombinedOutput() //nolint:noctx,gosec // the test's context has ended by its cleanup; G204: diskutil and hdiutil by their full paths, on the test's own folder
		if err == nil {
			if len(failed) > 0 {
				t.Logf("the image's cleanup, not the rule: %s came off by %s after %d tries failed:\n%s", mnt, strings.Join(way, " "), len(failed), strings.Join(failed, "\n"))
			}
			return
		}
		failed = append(failed, fmt.Sprintf("%s: %v: %s", strings.Join(way, " "), err, bytes.TrimSpace(out)))
	}
	t.Errorf("the image's cleanup, not the rule: %s is still mounted, and every way to take it off failed:\n%s", mnt, strings.Join(failed, "\n"))
}

// mounted reports whether a volume is mounted on mnt itself.
func mounted(t testing.TB, mnt string) bool {
	t.Helper()
	var st unix.Statfs_t
	if err := unix.Statfs(mnt, &st); err != nil {
		t.Errorf("the image's cleanup, not the rule: %v", err)
		return false
	}
	return unix.ByteSliceToString(st.Mntonname[:]) == mnt
}

// An image diskutil fails to eject comes off by the next way, and the failures are logged, not
// the test's: ejects, with each diskutil try a way that fails, so only hdiutil's detach is left.
//
// What it costs when this fails: the owners-off test fails now and then on a busy Mac for its
// cleanup, not for the rule it checks, and a push waits on a retry that says nothing.
func TestAnImageAWayFailsToEjectComesOffByTheNextAndOnlyTheLogSaysSo(t *testing.T) {
	t.Parallel()
	mnt := attach(t, realDir(t), true)
	if !mounted(t, mnt) {
		t.Fatalf("%s is not mounted on after attach", mnt)
	}
	ways := ejects()
	for i, way := range ways {
		if way[0] == "/usr/sbin/diskutil" {
			ways[i] = []string{"/usr/bin/false"}
		}
	}
	got := &said{TB: t}
	eject(got, mnt, ways)
	if len(got.errs) != 0 {
		t.Errorf("a way that failed failed the test: %q, and want it logged", got.errs)
	}
	if len(got.logs) != 1 || !strings.Contains(got.logs[0], "came off by /usr/bin/hdiutil detach -force after 3 tries failed") {
		t.Errorf("the log is %q, and want one line that it came off by hdiutil after 3 tries failed", got.logs)
	}
	if mounted(t, mnt) {
		t.Errorf("%s is still mounted on, and want the image off by hdiutil", mnt)
	}
}

// said is a test's TB that keeps what eject logs and fails instead of saying it.
type said struct {
	testing.TB
	logs, errs []string
}

func (s *said) Logf(format string, args ...any) {
	s.logs = append(s.logs, fmt.Sprintf(format, args...))
}
func (s *said) Errorf(format string, args ...any) {
	s.errs = append(s.errs, fmt.Sprintf(format, args...))
}

// What it costs when this fails: on a Mac, a key on an external disk, mounted to ignore ownership,
// passes Path and ReadOwned as this account's 0600 file, while the volume shows every other
// account as its owner too, with the right to rewrite it.
func TestAKeyOnADiskImageMountedWithOwnersOffIsRefused(t *testing.T) {
	t.Parallel()
	me, err := Account()
	if err != nil {
		t.Fatal(err)
	}
	mnt := attach(t, realDir(t), false)
	key := filepath.Join(mnt, "key")
	if err := os.WriteFile(key, []byte("k\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(key); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the key is %v, %v, and want 0600", fi, err)
	}
	_, perr := Path(key, Key, Who{})
	_, rerr := ReadOwned(key, me, true, 64)
	for _, c := range []struct {
		name, at string
		err      error
	}{{"Path", mnt, perr}, {"ReadOwned", key, rerr}} {
		u, ok := errors.AsType[*Unsafe](c.err)
		if !ok || !errors.Is(c.err, ErrOwnersIgnored) || !errors.Is(c.err, ErrNotOwners) || u.Fix != (Fix{}) ||
			!strings.HasPrefix(u.Observed, c.at+" is on the volume mounted on "+mnt+",") {
			t.Errorf("%s of a 0600 key on an image with owners off: %v, and want an *Unsafe at %s matching ErrOwnersIgnored and naming %s", c.name, c.err, c.at, mnt)
		}
	}
}

// What it costs when this fails: on a Mac the mounter of a volume is not read, so a disk image or
// FUSE volume another account attached passes on the owners it reports. The rule
// is TestANameOnAVolumeAnAccountNotTrustedMountedIsRefused's; this reads f_owner on a real disk:
// root's for the disk the tests write on, this account's for an image it attaches.
func TestTheMounterOfAVolumeIsReadByStatfs(t *testing.T) {
	t.Parallel()
	me, err := Account()
	if err != nil {
		t.Fatal(err)
	}
	d := realDir(t)
	mnt := attach(t, d, true)
	for _, c := range []struct{ path, who, on string }{{d, "uid:0", ""}, {mnt, me, mnt}} {
		n, err := look(c.path)
		if err != nil || n.mounter != c.who || c.on != "" && n.mountedOn != c.on {
			t.Errorf("%s: mounted by %q on %q, %v, and want %q", c.path, n.mounter, n.mountedOn, err, c.who)
		}
	}
	if _, err := Path(mnt, Folder, Who{}); err != nil {
		t.Errorf("the folder of an image this account attached: %v, and want a pass", err)
	}
}

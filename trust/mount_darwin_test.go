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
	"time"

	"golang.org/x/sys/unix"

	"github.com/birdtable/go-files/internal/imagetest"
)

// attach makes an 8 MB APFS disk image in d and mounts it on a new folder there, with owners off,
// as an external disk usually is, unless owners, and answers that folder and the image's path. It
// is detached when the test ends, before d is removed, however the test ends: the detach,
// imagetest.Detach, which confine's tests share, is registered before the attach runs and finds
// the image by its path, so an attach that fails after the image is on, a test that fails or
// panics after it, and a volume unmounted while its image stays attached each still leave no image
// attached (go-files#41); and it waits, bounded, for a volume the system holds to let go
// (go-files#55). diskutil is on every Mac, so nothing here skips: a Mac that cannot attach one
// fails the test. The disk every other test here writes on keeps ownership, and their passing is
// the owners-off image's other half.
func attach(t *testing.T, d string, owners bool) (mnt, img string) {
	t.Helper()
	img, mnt = filepath.Join(d, "ext.dmg"), filepath.Join(d, "ext")
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
	t.Cleanup(func() { imagetest.Detach(t, img, mnt, imagetest.Ejects()) })
	if out, err := exec.CommandContext(t.Context(), "/usr/sbin/diskutil", args...).CombinedOutput(); err != nil { //nolint:gosec // G204: diskutil by its full path, on the test's own folder
		t.Fatalf("diskutil image attach: %v: %s", err, out)
	}
	var st unix.Statfs_t
	if err := unix.Statfs(mnt, &st); err != nil || (st.Flags&unix.MNT_IGNORE_OWNERSHIP == 0) != owners {
		t.Fatalf("the image on %s is mounted with the flags %#x, %v, and want MNT_IGNORE_OWNERSHIP set: %t", mnt, st.Flags, err, !owners)
	}
	return mnt, img
}

// mounted reports whether a volume is mounted on mnt itself.
func mounted(t testing.TB, mnt string) bool {
	t.Helper()
	var st unix.Statfs_t
	if err := unix.Statfs(mnt, &st); err != nil {
		t.Errorf("%v", err)
		return false
	}
	return unix.ByteSliceToString(st.Mntonname[:]) == mnt
}

// An image diskutil fails to eject comes off by the next way, and the failures are logged, not
// the test's: imagetest.Ejects with each diskutil try a way that fails, and no time, so only
// hdiutil's detach is left, at once. An image that no way takes off by the deadline fails the
// test, after waits on its volume, and the failure names its disk, the volume still mounted on its
// folder and lsof of it. How the waits run is imagetest's own tests'.
//
// What it costs when this fails: the owners-off test fails now and then on a busy Mac for its
// cleanup, not for the rule it checks, and a push waits on a retry that says nothing; or an
// image left attached passes the test, and stays attached for days (go-files#41).
func TestAnImageAWayFailsToEjectComesOffByTheNextAndOnlyTheLogSaysSo(t *testing.T) {
	t.Parallel()
	mnt, img := attach(t, realDir(t), true)
	if !mounted(t, mnt) {
		t.Fatalf("%s is not mounted on after attach", mnt)
	}
	stuck := &said{TB: t}
	imagetest.Detach(stuck, img, mnt, imagetest.Plan{Ways: [][]string{{"/usr/bin/false"}}, Within: 2 * time.Second, Gap: 500 * time.Millisecond})
	if len(stuck.errs) != 1 {
		t.Fatalf("no way took the image off, and the test failed with %q, and want one failure", stuck.errs)
	}
	for _, part := range []string{img + " is still attached as /dev/disk", fmt.Sprintf("mounted on or beneath %s: %q", mnt, []string{mnt}), "lsof +f -- " + mnt + " ("} {
		if !strings.Contains(stuck.errs[0], part) {
			t.Errorf("no way took the image off, and the failure is %q, and want it to say %q", stuck.errs[0], part)
		}
	}
	p := imagetest.Ejects()
	for i, way := range p.Ways {
		if way[0] == "/usr/sbin/diskutil" {
			p.Ways[i] = []string{"/usr/bin/false"}
		}
	}
	p.Within = 0
	got := &said{TB: t}
	imagetest.Detach(got, img, mnt, p)
	if len(got.errs) != 0 {
		t.Errorf("a way that failed failed the test: %q, and want it logged", got.errs)
	}
	if len(got.logs) != 1 || !strings.Contains(got.logs[0], "came off by /usr/bin/hdiutil detach -force after 3 tries failed") {
		t.Errorf("the log is %q, and want one line that it came off by hdiutil after 3 tries failed", got.logs)
	}
	if disk, err := imagetest.Disk(img); disk != "" || err != nil {
		t.Errorf("%s is still attached as %q (%v), and want the image off by hdiutil", img, disk, err)
	}
}

// attachHelper, as the third argument from the end of this test binary's own, starts
// TestAnImageIsDetachedHoweverItsTestEnds as the test that attaches an image and then ends as
// the next argument says, in the folder after it.
const attachHelper = "attach-helper"

// An image a test attached is detached when the test then fails, panics, or passes with the
// volume unmounted and the image still attached: the test binary is started again as such a
// test, in this test's folder, and once it has ended hdiutil names no image from there.
//
// What it costs when this fails: a test that ends any other way than a pass leaves its disk
// image attached for days, its folder held, and every later run of the tests on that Mac with
// one more disk; on 25 Nov two did (go-files#41).
func TestAnImageIsDetachedHoweverItsTestEnds(t *testing.T) {
	t.Parallel()
	if a := os.Args; len(a) > 3 && a[len(a)-3] == attachHelper {
		attachThenEnd(t, a[len(a)-2], a[len(a)-1])
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		how    string
		passes bool
	}{{"fails", false}, {"panics", false}, {"unmounts", true}} {
		t.Run(c.how, func(t *testing.T) {
			t.Parallel()
			d := realDir(t)
			img := filepath.Join(d, "ext.dmg")
			// so a red here leaves nothing attached either
			t.Cleanup(func() { imagetest.Detach(t, img, filepath.Join(d, "ext"), imagetest.Ejects()) })
			out, err := exec.CommandContext(t.Context(), exe, "-test.run=^TestAnImageIsDetachedHoweverItsTestEnds$", "-test.count=1", "--", attachHelper, c.how, d).CombinedOutput() //nolint:gosec // this test binary again, with the test's own arguments
			if (err == nil) != c.passes || !bytes.Contains(out, []byte("attached on "+filepath.Join(d, "ext")+"\n")) {
				t.Fatalf("the helper that %s after the attach ended %v, and want it to attach and then pass: %t:\n%s", c.how, err, c.passes, out)
			}
			if disk, err := imagetest.Disk(img); disk != "" || err != nil {
				t.Errorf("after a test that %s after the attach, %s is still attached as %q (%v), and want no image attached", c.how, img, disk, err)
			}
		})
	}
}

// attachThenEnd attaches an image in d, says so on its stdout, and then ends the test as how says.
func attachThenEnd(t *testing.T, how, d string) {
	t.Helper()
	mnt, _ := attach(t, d, true)
	fmt.Fprintln(os.Stdout, "attached on "+mnt) //nolint:forbidigo // the helper tells the test it attached on its stdout, which the test reads
	switch how {
	case "fails":
		t.Fatal("a test that fails after the attach")
	case "panics":
		panic("a test that panics after the attach")
	case "unmounts":
		if out, err := exec.CommandContext(t.Context(), "/usr/sbin/diskutil", "unmount", mnt).CombinedOutput(); err != nil { //nolint:gosec // G204: diskutil by its full path, on the test's own folder
			t.Fatalf("diskutil unmount: %v: %s", err, out)
		}
	default:
		t.Fatalf("unknown way to end %q", how)
	}
}

// said is a test's TB that keeps what detach logs and fails instead of saying it.
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
	mnt, _ := attach(t, realDir(t), false)
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
	mnt, _ := attach(t, d, true)
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

package imagetest

import (
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test makes, attaches and unmounts a disk image with hdiutil and diskutil; the module has no runner
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// On this Mac the wait between two ways wakes when the volume on the image's folder is unmounted,
// and ends there: the kqueue on the mount table is woken by the unmount, and getfsstat then lists
// nothing on the folder. An image is attached, the wait begins while it is mounted, and its first
// wait unmounts it; it must end on that one wake, not at the end of its gap. The image's own
// cleanup, Detach, then takes the unmounted image off by its disk. hdiutil and diskutil are on
// every Mac, so nothing here skips.
//
// What it costs when this fails: the wait never wakes on the mount table, so every wait between
// two ways sits out its whole gap, and a cleanup that waits on a volume to let go never sees it.
func TestOnMacOSTheWaitWakesWhenTheVolumeIsUnmounted(t *testing.T) {
	t.Parallel()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	img, mnt := filepath.Join(d, "ext.dmg"), filepath.Join(d, "ext")
	if out, err := exec.CommandContext(t.Context(), "/usr/bin/hdiutil", "create", "-fs", "APFS", "-size", "8m", "-volname", "ext", img).CombinedOutput(); err != nil { //nolint:gosec // G204: hdiutil by its full path, on the test's own folder
		t.Fatalf("hdiutil create: %v: %s", err, out)
	}
	if err := os.Mkdir(mnt, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Detach(t, img, mnt, Ejects()) })
	if out, err := exec.CommandContext(t.Context(), "/usr/bin/hdiutil", "attach", "-nobrowse", "-mountpoint", mnt, img).CombinedOutput(); err != nil { //nolint:gosec // G204: hdiutil by its full path, on the test's own folder
		t.Fatalf("hdiutil attach: %v: %s", err, out)
	}
	if on, err := mountsBeneath(mnt); err != nil || !slices.Equal(on, []string{mnt}) {
		t.Fatalf("getfsstat lists %q (%v) on or beneath %s after the attach, and want it alone", on, err, mnt)
	}

	h, done := system(img, mnt)
	defer func() {
		if err := done(); err != nil {
			t.Error(err)
		}
	}()
	var woke []bool
	wait := h.wait
	h.wait = func(until time.Time) (bool, error) {
		if len(woke) == 0 {
			if out, err := exec.CommandContext(t.Context(), "/usr/sbin/diskutil", "unmount", mnt).CombinedOutput(); err != nil { //nolint:gosec // G204: diskutil by its full path, on the test's own folder
				t.Fatalf("diskutil unmount: %v: %s", err, out)
			}
		}
		changed, err := wait(until)
		woke = append(woke, changed)
		return changed, err
	}
	if err := settle(h, h.now().Add(time.Hour), 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(woke, []bool{true}) {
		t.Errorf("the waits woke %v, and want one, woken by the unmount", woke)
	}
	if on, err := mountsBeneath(mnt); err != nil || len(on) != 0 {
		t.Errorf("getfsstat lists %q (%v) on or beneath %s once the wait ended, and want nothing", on, err, mnt)
	}
	if got := holders(mnt); !strings.HasPrefix(got, "nothing is mounted there") {
		t.Errorf("the holders of an unmounted image are %q, and want that nothing is mounted, with no lsof of the volume beneath", got)
	}
}

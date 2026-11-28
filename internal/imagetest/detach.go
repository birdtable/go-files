package imagetest

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Plan is how a detach takes an image off: the ways it tries, in order, each a program by its full
// path and its arguments, given the image's own disk last; the deadline from its start, Within;
// and the most one wait between two ways lasts, Gap. Every way is tried once, however short
// Within is; after the last, it is tried again after each wait, until Within has passed.
type Plan struct {
	Ways   [][]string
	Within time.Duration
	Gap    time.Duration
}

// Ejects is the plan every image's cleanup takes: diskutil's forced eject, three times, as it fails
// once in a while on a busy Mac ("Resource busy") and passes on the next try, and last hdiutil's
// forced detach, which goes to the image's device by another road and takes it off even when busy,
// tried again until 30 s have passed. diskarbitrationd and the system's own disk helpers hold a
// volume just used for a short while (go-files#55), so a wait between two ways, of 3 s at most,
// lets them go first.
func Ejects() Plan {
	return Plan{
		Ways: [][]string{
			{"/usr/sbin/diskutil", "eject", "force"},
			{"/usr/sbin/diskutil", "eject", "force"},
			{"/usr/sbin/diskutil", "eject", "force"},
			{"/usr/bin/hdiutil", "detach", "-force"},
		},
		Within: 30 * time.Second,
		Gap:    3 * time.Second,
	}
}

// host is what a detach asks the system: run a way on the image's disk; the image's own disk, or
// "" when it is off; every mount point on or beneath the image's folder; wait until the mount
// table may have changed, or until a time, whichever comes first, and say which; the time; and
// what holds the image's folder, for a red. The system's is in the files of each system.
type host struct {
	run     func(way []string, disk string) ([]byte, error)
	disk    func() (string, error)
	mounted func() ([]string, error)
	wait    func(until time.Time) (changed bool, err error)
	now     func() time.Time
	holders func() string
}

// detach takes the image at img off by the first of p's ways that does, as Plan says. A way that
// passes and leaves the image attached (an eject that unmounted the volume and kept the disk)
// counts as failed. Between two ways it waits, as settle says, never past the deadline. Tries that
// failed before the image came off are logged, not failed: the rule was judged before the cleanup,
// and a cleanup that got there is not the rule's result. When the image is still attached at the
// deadline, the test fails, says it is the image's cleanup, not the rule, and names what is
// mounted on or beneath mnt and what holds it. An image not attached is left as it is.
func detach(t testing.TB, h host, img, mnt string, p Plan) {
	t.Helper()
	until := h.now().Add(p.Within)
	disk, err := h.disk()
	var failed []string
	for i := 0; err == nil && disk != "" && len(p.Ways) > 0; i++ {
		if i >= len(p.Ways) && !h.now().Before(until) {
			break
		}
		if i > 0 {
			if err = settle(h, until, p.Gap); err != nil {
				break
			}
		}
		way := p.Ways[min(i, len(p.Ways)-1)]
		out, runErr := h.run(way, disk)
		was := disk
		if disk, err = h.disk(); err == nil && disk == "" {
			if len(failed) > 0 {
				t.Logf("the image's cleanup, not the rule: %s came off by %s after %d tries failed:\n%s", img, strings.Join(way, " "), len(failed), strings.Join(failed, "\n"))
			}
			return
		}
		if runErr == nil {
			runErr = errors.New("it passed, and the image is still attached")
		}
		failed = append(failed, fmt.Sprintf("%s %s: %v: %s", strings.Join(way, " "), was, runErr, strings.TrimSpace(string(out))))
	}
	switch {
	case err != nil:
		t.Errorf("the image's cleanup, not the rule: %s: %v; tries that failed:\n%s", img, err, strings.Join(failed, "\n"))
	case disk != "":
		on, merr := h.mounted()
		t.Errorf("the image's cleanup, not the rule: %s is still attached as %s %s after it was first tried, and every way to take it off failed:\n%s\nmounted on or beneath %s: %q (%v); what holds it:\n%s",
			img, disk, p.Within, strings.Join(failed, "\n"), mnt, on, merr, h.holders())
	}
}

// settle is the wait between two ways, which ends on the first of three: nothing is mounted on or
// beneath the image's folder any more, when something was as it began (the volume let go, so the
// next way goes at once); gap has passed; or the deadline has. It waits on the mount table, not by
// a fixed pause: each change wakes it to look again. When nothing was mounted as it began, the
// way failed on the disk alone, which the mount table does not show, and it waits out the gap.
func settle(h host, until time.Time, gap time.Duration) error {
	end := h.now().Add(gap)
	if until.Before(end) {
		end = until
	}
	on, err := h.mounted()
	if err != nil {
		return err
	}
	held := len(on) > 0
	for h.now().Before(end) {
		changed, err := h.wait(end)
		if err != nil {
			return err
		}
		if !changed || !held {
			continue
		}
		if on, err = h.mounted(); err != nil || len(on) == 0 {
			return err
		}
	}
	return nil
}

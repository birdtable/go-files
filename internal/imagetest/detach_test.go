package imagetest

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// busy is a host whose image is held by the system's disk helpers until freeAt from the first
// try, as diskarbitrationd holds a volume just used: every way before then answers "Resource
// busy", and the first at or after it takes the image off. Its volume is mounted on mnt until
// unmountAt, or never when that is 0, or from the start not at all when gone. Its clock moves only
// in its waits, so what it records is a count of work at times it set, never the machine's.
type busy struct {
	t        *testing.T
	start    time.Time
	clock    time.Time
	freeAt   time.Duration
	unmount  time.Duration
	gone     bool
	off      bool
	tries    []time.Duration
	waits    int
	holderOf string
}

const (
	mnt  = "/test/ext"
	img  = "/test/ext.dmg"
	disk = "/dev/disk9"
)

func newBusy(t *testing.T, freeAt time.Duration) *busy {
	t.Helper()
	start := time.Unix(1_800_000_000, 0)
	return &busy{t: t, start: start, clock: start, freeAt: freeAt, holderOf: "lsof +f -- " + mnt + ": mds_stores 99 holds it"}
}

func (b *busy) elapsed() time.Duration { return b.clock.Sub(b.start) }

func (b *busy) mountedNow() bool {
	return !b.off && !b.gone && (b.unmount == 0 || b.elapsed() < b.unmount)
}

func (b *busy) host() host {
	return host{
		run: func(way []string, d string) ([]byte, error) {
			if d != disk {
				b.t.Fatalf("a way was given %q, and want the image's own disk %s", d, disk)
			}
			if b.tries = append(b.tries, b.elapsed()); len(b.tries) > 100 {
				b.t.Fatalf("a hundred tries, and still trying: the deadline does not end them")
			}
			if b.elapsed() >= b.freeAt {
				b.off = true
				return nil, nil
			}
			return []byte(way[0] + ": Resource busy"), errors.New("exit status 1")
		},
		disk: func() (string, error) {
			if b.off {
				return "", nil
			}
			return disk, nil
		},
		mounted: func() ([]string, error) {
			if b.mountedNow() {
				return []string{mnt}, nil
			}
			return nil, nil
		},
		wait: func(until time.Time) (bool, error) {
			b.waits++
			if at := b.start.Add(b.unmount); b.mountedNow() && b.unmount > 0 && at.Before(until) {
				b.clock = at
				return true, nil
			}
			b.clock = until
			return false, nil
		},
		now:     func() time.Time { return b.clock },
		holders: func() string { return b.holderOf },
	}
}

func seconds(s ...float64) []time.Duration {
	d := make([]time.Duration, len(s))
	for i, v := range s {
		d[i] = time.Duration(v * float64(time.Second))
	}
	return d
}

// A busy volume that frees itself before the deadline comes off with no red: Ejects' ways go in
// order with a wait of its gap between two, and once they are spent hdiutil's detach is tried again
// after each wait, so a volume held past the last way still comes off. The tries that failed are
// logged, not failed.
//
// What it costs when this fails: a volume the system's disk helpers hold for a few seconds fails
// trust's image tests for their cleanup, not for the rule they check, on a macOS run that then has
// to be retried and says nothing (go-files PR 52, run 46143820627, attempt 3).
func TestABusyVolumeThatFreesItselfWithinTheDeadlineComesOffWithNoRed(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		freeAt time.Duration
		tries  []time.Duration
		log    string
	}{
		{7 * time.Second, seconds(0, 3, 6, 9), "came off by /usr/bin/hdiutil detach -force after 3 tries failed"},
		{14 * time.Second, seconds(0, 3, 6, 9, 12, 15), "came off by /usr/bin/hdiutil detach -force after 5 tries failed"},
		{29 * time.Second, seconds(0, 3, 6, 9, 12, 15, 18, 21, 24, 27, 30), "came off by /usr/bin/hdiutil detach -force after 10 tries failed"},
	} {
		t.Run(c.freeAt.String(), func(t *testing.T) {
			t.Parallel()
			b := newBusy(t, c.freeAt)
			got := &said{TB: t}
			detach(got, b.host(), img, mnt, Ejects())
			if len(got.errs) != 0 {
				t.Errorf("a volume freed at %s failed the test: %q, and want it off with no red", c.freeAt, got.errs)
			}
			if !slices.Equal(b.tries, c.tries) {
				t.Errorf("the ways were tried at %v, and want %v", b.tries, c.tries)
			}
			if len(got.logs) != 1 || !strings.Contains(got.logs[0], c.log) {
				t.Errorf("the log is %q, and want one line that it %s", got.logs, c.log)
			}
		})
	}
}

// A volume that never frees is red once the deadline has passed, and the red names the image's
// disk, every try, what is still mounted on or beneath its folder, and what holds it.
//
// What it costs when this fails: an image left attached passes the test, and stays attached for
// days (go-files#41); or a red says only that it failed, and the run that showed what held the
// volume is gone before anyone reads it.
func TestAVolumeThatNeverFreesIsRedAtTheDeadlineWithItsHolderNamed(t *testing.T) {
	t.Parallel()
	b := newBusy(t, time.Hour)
	got := &said{TB: t}
	detach(got, b.host(), img, mnt, Ejects())
	if want := seconds(0, 3, 6, 9, 12, 15, 18, 21, 24, 27, 30); !slices.Equal(b.tries, want) {
		t.Errorf("the ways were tried at %v, and want %v, the last at the deadline", b.tries, want)
	}
	if len(got.errs) != 1 {
		t.Fatalf("the test failed with %q, and want one failure", got.errs)
	}
	for _, part := range []string{
		img + " is still attached as " + disk + " 30s after",
		"/usr/bin/hdiutil detach -force " + disk + ": exit status 1: /usr/bin/hdiutil: Resource busy",
		fmt.Sprintf("mounted on or beneath %s: %q", mnt, []string{mnt}),
		b.holderOf,
	} {
		if !strings.Contains(got.errs[0], part) {
			t.Errorf("the red is %q, and want it to say %q", got.errs[0], part)
		}
	}
	if n := strings.Count(got.errs[0], "Resource busy"); n != 11 {
		t.Errorf("the red names %d tries, and want all 11", n)
	}
}

// The wait between two ways ends as soon as the volume on the image's folder lets go, as getfsstat
// shows it, not at the gap's end: it waits on the condition, not for a fixed pause. Where nothing
// was mounted as it began, the disk alone is held, which the mount table does not show, and the
// wait lasts the gap.
//
// What it costs when this fails: each cleanup that waits sits out the whole gap after the volume
// is gone, and a red test's cleanup takes its full deadline; or a wait that ends on any change at
// all tries the next way into a volume still held, and the gap is spent in a row of busy tries.
func TestTheWaitBetweenTwoWaysEndsWhenTheVolumeLetsGo(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name          string
		unmount, free time.Duration
		gone          bool
		tries         []time.Duration
	}{
		{"unmounted at 1.5s", 1500 * time.Millisecond, 1500 * time.Millisecond, false, seconds(0, 1.5)},
		{"unmounted at 4s", 4 * time.Second, 4 * time.Second, false, seconds(0, 3, 4)},
		{"never mounted", 0, 4 * time.Second, true, seconds(0, 3, 6)},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			b := newBusy(t, c.free)
			b.unmount, b.gone = c.unmount, c.gone
			got := &said{TB: t}
			detach(got, b.host(), img, mnt, Ejects())
			if len(got.errs) != 0 || !slices.Equal(b.tries, c.tries) {
				t.Errorf("the ways were tried at %v, with the failures %q, and want them at %v with none", b.tries, got.errs, c.tries)
			}
		})
	}
}

// Every way is tried once however short the deadline, back to back when it has passed: a plan of
// no time is the ways with no wait, and the red still comes.
//
// What it costs when this fails: a deadline spent before the last way skips hdiutil's detach, the
// one way that takes a busy image off, and an image comes off red that the next try would have
// taken off.
func TestEveryWayIsTriedOnceHoweverShortTheDeadline(t *testing.T) {
	t.Parallel()
	b := newBusy(t, time.Hour)
	p := Ejects()
	p.Within = 0
	got := &said{TB: t}
	detach(got, b.host(), img, mnt, p)
	if !slices.Equal(b.tries, seconds(0, 0, 0, 0)) || b.waits != 0 || len(got.errs) != 1 {
		t.Errorf("tried at %v after %d waits, failing %q, and want each of the 4 ways once, with no wait, and one failure", b.tries, b.waits, got.errs)
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

package imagetest

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os/exec" //nolint:depguard // cli.process.1: the tests take their disk images off with diskutil and hdiutil; the module has no runner
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Detach takes the image at img, attached on the folder mnt, off by p, as detach says, and fails t
// if it is still attached at p's deadline. A test registers it as a cleanup before the attach runs,
// so the image comes off however the test ends; it finds the image by its path, so an image whose
// volume was unmounted, or never mounted, comes off too (go-files#41).
func Detach(t testing.TB, img, mnt string, p Plan) {
	t.Helper()
	h, done := system(img, mnt)
	defer func() {
		if err := done(); err != nil {
			t.Errorf("the image's cleanup, not the rule: %v", err)
		}
	}()
	detach(t, h, img, mnt, p)
}

// system is the host of a detach of img on mnt on this Mac, and what ends it. Its wait is a kqueue
// on the mount table (EVFILT_FS), made before anything is asked, so a volume that goes between a
// look and the wait still wakes it. A kqueue the system will not make is the error of every wait,
// so the detach tries each way once, with no wait, and is red with it.
func system(img, mnt string) (host, func() error) {
	w, err := watchMounts()
	wait := w.wait
	if err != nil {
		wait = func(time.Time) (bool, error) { return false, err }
	}
	return host{
		run: func(way []string, disk string) ([]byte, error) {
			return exec.Command(way[0], append(way[1:len(way):len(way)], disk)...).CombinedOutput() //nolint:noctx,gosec // run from cleanups, after the test's context has ended; G204: a way by its full path, on the test's own image
		},
		disk:    func() (string, error) { return Disk(img) },
		mounted: func() ([]string, error) { return mountsBeneath(mnt) },
		wait:    wait,
		now:     time.Now,
		holders: func() string { return holders(mnt) },
	}, w.close
}

// mounts is a kqueue that wakes on every change to the mount table, or -1 when there is none.
type mounts struct{ kq int }

// watchMounts makes a kqueue that wakes on every change to the mount table: a volume mounted,
// unmounted or gone. Every process may make one; no disk daemon is asked.
func watchMounts() (mounts, error) {
	kq, err := unix.Kqueue()
	if err != nil {
		return mounts{kq: -1}, fmt.Errorf("kqueue: %w", err)
	}
	var ev unix.Kevent_t
	unix.SetKevent(&ev, 0, unix.EVFILT_FS, unix.EV_ADD|unix.EV_CLEAR)
	if _, err := unix.Kevent(kq, []unix.Kevent_t{ev}, nil, nil); err != nil {
		return mounts{kq: -1}, errors.Join(fmt.Errorf("kevent, adding EVFILT_FS: %w", err), unix.Close(kq))
	}
	return mounts{kq: kq}, nil
}

// wait returns when the mount table has changed since the last wait, or at until, whichever comes
// first, and says whether it changed. An interrupted wait counts as a change: the caller looks
// again, and waits again if nothing went.
func (m mounts) wait(until time.Time) (bool, error) {
	d := time.Until(until)
	if d <= 0 {
		return false, nil
	}
	ts := unix.NsecToTimespec(d.Nanoseconds())
	var got [8]unix.Kevent_t
	n, err := unix.Kevent(m.kq, nil, got[:], &ts)
	switch {
	case errors.Is(err, unix.EINTR):
		return true, nil
	case err != nil:
		return false, fmt.Errorf("kevent, waiting on the mount table: %w", err)
	}
	return n > 0, nil
}

// close lets the kqueue go, if there is one.
func (m mounts) close() error {
	if m.kq < 0 {
		return nil
	}
	if err := unix.Close(m.kq); err != nil {
		return fmt.Errorf("closing the kqueue on the mount table: %w", err)
	}
	return nil
}

// mountsBeneath is every mount point on or beneath mnt that getfsstat lists, the deepest first.
func mountsBeneath(mnt string) ([]string, error) {
	n, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil {
		return nil, fmt.Errorf("getfsstat: %w", err)
	}
	all := make([]unix.Statfs_t, n)
	if n, err = unix.Getfsstat(all, unix.MNT_NOWAIT); err != nil {
		return nil, fmt.Errorf("getfsstat: %w", err)
	}
	var on []string
	for _, m := range all[:n] {
		if at := unix.ByteSliceToString(m.Mntonname[:]); at == mnt || strings.HasPrefix(at, mnt+"/") {
			on = append(on, at)
		}
	}
	slices.SortFunc(on, func(a, b string) int { return len(b) - len(a) })
	return on, nil
}

// holders is what lsof says this account has open on each volume mounted on or beneath mnt, for a
// red. Where nothing is mounted lsof is not asked, as it would list the volume mnt is on: the image
// is then held by its disk alone, which the log says.
func holders(mnt string) string {
	on, err := mountsBeneath(mnt)
	if err != nil {
		return err.Error()
	}
	if len(on) == 0 {
		return "nothing is mounted there, so its disk alone is held, by no file this account has open"
	}
	var b strings.Builder
	for _, at := range on {
		out, err := exec.Command("/usr/sbin/lsof", "-n", "-P", "+f", "--", at).CombinedOutput() //nolint:noctx,gosec // run from cleanups, after the test's context has ended; G204: lsof by its full path, on the test's own mount point
		fmt.Fprintf(&b, "lsof +f -- %s (%v):\n%s", at, err, out)
	}
	return b.String()
}

// Disk is the disk hdiutil info names first for the image attached from img, its own, or "" when
// no image from img is attached.
func Disk(img string) (string, error) {
	out, err := exec.Command("/usr/bin/hdiutil", "info", "-plist").Output() //nolint:noctx // run from cleanups too, after the test's context has ended
	if err != nil {
		return "", fmt.Errorf("hdiutil info: %w", err)
	}
	var doc plist
	if err := xml.Unmarshal(out, &doc); err != nil || len(doc.Items) != 1 {
		return "", fmt.Errorf("hdiutil info -plist is not one plist: %w", err)
	}
	images, _ := doc.Items[0].get("images")
	for _, image := range images.Items {
		if path, _ := image.get("image-path"); path.Text != img {
			continue
		}
		entities, _ := image.get("system-entities")
		for _, e := range entities.Items {
			if dev, ok := e.get("dev-entry"); ok && dev.Text != "" {
				return dev.Text, nil
			}
		}
		return "", fmt.Errorf("hdiutil info names the image %s attached, with no disk", img)
	}
	return "", nil
}

// plist is a node of a property list as hdiutil writes one: an element, its text, and its children.
type plist struct {
	XMLName xml.Name
	Text    string  `xml:",chardata"`
	Items   []plist `xml:",any"`
}

// get is the value after the key named in the dict p, and whether it is there.
func (p plist) get(key string) (plist, bool) {
	for i := 0; i+1 < len(p.Items); i += 2 {
		if p.Items[i].XMLName.Local == "key" && p.Items[i].Text == key {
			return p.Items[i+1], true
		}
	}
	return plist{}, false
}

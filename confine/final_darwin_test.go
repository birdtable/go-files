package confine

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test makes and attaches an HFS+ disk image with hdiutil; the module has no runner
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// finalFoldersOnly is whether Final answers only a folder here, and refuses a file: macOS does, as F_GETPATH answers the name of a file looked at last.
const finalFoldersOnly = true

func TestOnMacOSFinalAnswersOneNameThroughTheDataVolumesFirmlink(t *testing.T) {
	t.Parallel()
	tmp := finalTemp(t)
	data := "/System/Volumes/Data" + tmp
	if got := finalOK(t, data); got != tmp {
		t.Errorf("Final(%q) = %q, and want the name through the firmlink %q", data, got, tmp)
	}
}

func TestOnMacOSFinalOfANameThisAccountMayNotReadIsErrPermission(t *testing.T) {
	t.Parallel()
	tmp := finalTemp(t)
	file, folder := filepath.Join(tmp, "file"), filepath.Join(tmp, "folder")
	if err := os.WriteFile(file, nil, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(folder, 0o300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(folder, 0o700) })
	for _, p := range []string{file, folder} {
		got, err := Final(p)
		if os.Geteuid() == 0 {
			if p == file {
				if !errors.Is(err, errors.ErrUnsupported) || got != "" {
					t.Errorf("Final of %s as root, no folder = %q, %v, and want errors.ErrUnsupported", p, got, err)
				}
				continue
			}
			if err != nil || got != p {
				t.Errorf("Final of %s as root = %q, %v, and want its own name", p, got, err)
			}
			continue
		}
		if pe, ok := errors.AsType[*fs.PathError](err); !ok || pe.Path != p || !errors.Is(err, fs.ErrPermission) {
			t.Errorf("Final of %s, which this account may not read: %v, and want an *fs.PathError naming it, matching fs.ErrPermission", p, err)
		}
	}
}

// volume makes a 20 MB disk image img of the file system kind, hdiutil's -fs, which needs no root,
// attaches it on mnt, a folder it makes, and checks that fstatfs names mnt's volume want. It is
// detached when the test ends, however the test ends, by detach: the detach is registered before
// the attach runs and finds the image by its path, as trust's tests do (go-files#41), and as
// cleanups run last registered first, a volume attached beneath another comes off before it, so
// the outer image is never held busy by the inner. hdiutil is on every Mac, so nothing here skips:
// a Mac that cannot make or attach one fails the test.
func volume(t *testing.T, kind, want, img, mnt string) {
	t.Helper()
	if out, err := exec.CommandContext(t.Context(), "/usr/bin/hdiutil", "create", "-fs", kind, "-size", "20m", "-volname", "final", img).CombinedOutput(); err != nil { //nolint:gosec // G204: hdiutil by its full path, on the test's own folder
		t.Fatalf("hdiutil create -fs %s: %v: %s", kind, err, out)
	}
	if err := os.Mkdir(mnt, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { detach(t, img, ejects()) })
	if out, err := exec.CommandContext(t.Context(), "/usr/bin/hdiutil", "attach", "-nobrowse", "-mountpoint", mnt, img).CombinedOutput(); err != nil { //nolint:gosec // G204: hdiutil by its full path, on the test's own folder
		t.Fatalf("hdiutil attach: %v: %s", err, out)
	}
	var st unix.Statfs_t
	if err := unix.Statfs(mnt, &st); err != nil || unix.ByteSliceToString(st.Fstypename[:]) != want || unix.ByteSliceToString(st.Mntonname[:]) != mnt {
		t.Fatalf("the image on %s: type %q on %q, %v, and want %s mounted on it", mnt, unix.ByteSliceToString(st.Fstypename[:]), unix.ByteSliceToString(st.Mntonname[:]), err, want)
	}
}

// ejects are the ways tried, in order, to take an image off, as trust's tests try them: diskutil's
// forced eject, three times, as it fails once in a while on a busy Mac ("Resource busy") and
// passes on the next try, and last hdiutil's forced detach, which goes to the image's device by
// another road and takes it off even when busy. There is no wait between tries: each one is a
// round trip to the system's disk daemon.
func ejects() [][]string {
	return [][]string{
		{"/usr/sbin/diskutil", "eject", "force"},
		{"/usr/sbin/diskutil", "eject", "force"},
		{"/usr/sbin/diskutil", "eject", "force"},
		{"/usr/bin/hdiutil", "detach", "-force"},
	}
}

// detach takes the image at img off by the first of ways that does, each given the image's own
// disk last, as hdiutil names it; it is trust's tests' detach. A way that passes and leaves the
// image attached counts as failed, and the next is tried. Tries that failed before the image came
// off are logged, not failed: the rule was judged before the cleanup. When the image is still
// attached after every way, the test fails, and says it is the image's cleanup, not the rule. An
// image not attached is left as it is.
func detach(t testing.TB, img string, ways [][]string) {
	t.Helper()
	disk, err := imageDisk(img)
	var failed []string
	for _, way := range ways {
		if err != nil || disk == "" {
			break
		}
		out, runErr := exec.Command(way[0], append(way[1:len(way):len(way)], disk)...).CombinedOutput() //nolint:noctx,gosec // the test's context has ended by its cleanup; G204: diskutil and hdiutil by their full paths, on the test's own image
		was := disk
		if disk, err = imageDisk(img); err == nil && disk == "" {
			if len(failed) > 0 {
				t.Logf("the image's cleanup, not the rule: %s came off by %s after %d tries failed:\n%s", img, strings.Join(way, " "), len(failed), strings.Join(failed, "\n"))
			}
			return
		}
		if runErr == nil {
			runErr = errors.New("it passed, and the image is still attached")
		}
		failed = append(failed, fmt.Sprintf("%s %s: %v: %s", strings.Join(way, " "), was, runErr, bytes.TrimSpace(out)))
	}
	switch {
	case err != nil:
		t.Errorf("the image's cleanup, not the rule: %v", err)
	case disk != "":
		t.Errorf("the image's cleanup, not the rule: %s is still attached as %s, and every way to take it off failed:\n%s", img, disk, strings.Join(failed, "\n"))
	}
}

// imageDisk is the disk hdiutil info names first for the image attached from img, its own, or ""
// when no image from img is attached.
func imageDisk(img string) (string, error) {
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

// An image attached beneath another's volume, as the nested test attaches APFS beneath HFS+, comes
// off before it when the test ends, and both are off once it has; an image no way takes off fails
// the test, and the failure names its disk.
//
// What it costs when this fails: the nested test fails now and then on a busy Mac for its cleanup,
// not for the rule it checks, as main did on 28 Nov ("Resource busy"); or an image left attached
// passes the test, and stays attached for days, its folder held (go-files#41).
func TestOnMacOSANestedImageComesOffBeforeTheOneItIsAttachedBeneath(t *testing.T) {
	t.Parallel()
	d := finalTemp(t)
	outer, inner := filepath.Join(d, "hfs.dmg"), filepath.Join(d, "apfs.dmg")
	t.Run("nested", func(t *testing.T) {
		mnt := filepath.Join(d, "hfs")
		volume(t, "JHFS+", "hfs", outer, mnt)
		volume(t, "APFS", "apfs", inner, filepath.Join(mnt, "m"))
		stuck := &said{TB: t}
		detach(stuck, inner, [][]string{{"/usr/bin/false"}})
		if len(stuck.errs) != 1 || !strings.Contains(stuck.errs[0], inner+" is still attached as /dev/disk") {
			t.Errorf("no way took the image off, and the test failed with %q, and want one failure that it is still attached", stuck.errs)
		}
	})
	for _, img := range []string{inner, outer} {
		if disk, err := imageDisk(img); disk != "" || err != nil {
			t.Errorf("after the test that attached it ended, %s is still attached as %q (%v), and want no image attached", img, disk, err)
		}
	}
}

// hfsLinked attaches a journaled HFS+ image on d/hfs (HFS+ gives a folder a second name only where
// it is journaled) and gives its folder d1/x a second name, d2/y, by link(2), as Time Machine
// does; it answers the volume, x and y.
func hfsLinked(t *testing.T, d string) (mnt, x, y string) {
	t.Helper()
	mnt = filepath.Join(d, "hfs")
	volume(t, "JHFS+", "hfs", filepath.Join(d, "hfs.dmg"), mnt)
	x, y = filepath.Join(mnt, "d1", "x"), filepath.Join(mnt, "d2", "y")
	for _, dir := range []string{x, filepath.Dir(y)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Link(x, y); err != nil {
		t.Fatalf("link of the folder %s as %s on HFS+: %v", x, y, err)
	}
	return mnt, x, y
}

// finalRefusedWhileLooked checks that Final refuses p, matching errors.ErrUnsupported, and so does
// final with other looked at between the open and the read, as any process may look at it, after
// which F_GETPATH answers by that name.
func finalRefusedWhileLooked(t *testing.T, p, other string) {
	t.Helper()
	if got, err := Final(p); !errors.Is(err, errors.ErrUnsupported) || got != "" {
		t.Errorf("Final of %s = %q, %v, and want errors.ErrUnsupported", p, got, err)
	}
	lookThenRead := func(f *os.File) (string, error) {
		if _, err := os.Lstat(other); err != nil {
			return "", err
		}
		return finalOf(f)
	}
	if got, err := final(p, lookThenRead, os.Lstat); !errors.Is(err, errors.ErrUnsupported) || got != "" {
		t.Errorf("Final of %s, while %s is looked at = %q, %v, and want errors.ErrUnsupported", p, other, got, err)
	}
}

// What it costs when this fails: on an HFS+ volume, as a Time Machine disk is, a folder with a
// second name answers, and so does every name beneath it, by whichever of the folder's names any
// process looked at last, in another folder than the path's: a journal keys one file by two names.
func TestOnMacOSFinalRefusesEveryNameOnAnHFSPlusVolume(t *testing.T) {
	t.Parallel()
	mnt, x, y := hfsLinked(t, finalTemp(t))
	file := filepath.Join(mnt, "file")
	for _, f := range []string{file, filepath.Join(x, "inside")} {
		if err := os.WriteFile(f, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct{ p, other string }{{mnt, y}, {file, y}, {x, y}, {filepath.Join(x, "inside"), y}, {y, x}} {
		finalRefusedWhileLooked(t, row.p, row.other)
	}
}

// What it costs when this fails: a volume of a listed type attached beneath an HFS+ folder with a
// second name, which any account may attach on a folder it owns, is named through that folder by
// whichever of its names was looked at last, so one file there answers by two names; and a volume
// of a type not listed answers a name that may be no key, as msdos and exfat answer the spelling a
// name was first looked up by, so one file keys by two names across a remount.
func TestOnMacOSFinalAnswersOnlyWhereEveryVolumeOnTheWayIsListed(t *testing.T) {
	t.Parallel()
	d := finalTemp(t)
	_, x, y := hfsLinked(t, d)
	apfs := filepath.Join(x, "m")
	volume(t, "APFS", "apfs", filepath.Join(d, "apfs.dmg"), apfs)
	file := filepath.Join(apfs, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	finalRefusedWhileLooked(t, file, y)
	finalRefusedWhileLooked(t, apfs, y)

	// A type not listed, the device file system, is refused, and so are the two FAT types beneath
	// APFS, which answer the spelling first looked up, not the stored one.
	if got, err := Final("/dev/null"); !errors.Is(err, errors.ErrUnsupported) || got != "" {
		t.Errorf("Final of /dev/null, on devfs = %q, %v, and want errors.ErrUnsupported", got, err)
	}
	for _, fat := range []struct{ kind, want string }{{"MS-DOS", "msdos"}, {"ExFAT", "exfat"}} {
		mnt := filepath.Join(d, fat.want)
		volume(t, fat.kind, fat.want, filepath.Join(d, fat.want+".dmg"), mnt)
		file := filepath.Join(mnt, "File")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{file, mnt} {
			if got, err := Final(p); !errors.Is(err, errors.ErrUnsupported) || got != "" {
				t.Errorf("Final of %s, on %s = %q, %v, and want errors.ErrUnsupported", p, fat.want, got, err)
			}
		}
	}
}

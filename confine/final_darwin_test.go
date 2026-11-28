package confine

import (
	"errors"
	"io/fs"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test makes and attaches an HFS+ disk image with hdiutil; the module has no runner
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// finalRefusesASecondName is whether Final refuses a file with a second name here: macOS refuses a file with a second name, as F_GETPATH answers the name looked at last.
const finalRefusesASecondName = true

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

// hfsVolume makes a 20 MB journaled HFS+ disk image in d with hdiutil, which needs no root (HFS+
// gives a folder a second name only where it is journaled), and attaches it on a new folder there,
// which it answers; it is detached when the test ends. hdiutil is on every Mac, so nothing here
// skips: a Mac that cannot make or attach one fails the test.
func hfsVolume(t *testing.T, d string) string {
	t.Helper()
	img, mnt := filepath.Join(d, "hfs.dmg"), filepath.Join(d, "hfs")
	if out, err := exec.CommandContext(t.Context(), "/usr/bin/hdiutil", "create", "-fs", "JHFS+", "-size", "20m", "-volname", "final", img).CombinedOutput(); err != nil { //nolint:gosec // G204: hdiutil by its full path, on the test's own folder
		t.Fatalf("hdiutil create: %v: %s", err, out)
	}
	if err := os.Mkdir(mnt, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.CommandContext(t.Context(), "/usr/bin/hdiutil", "attach", "-nobrowse", "-mountpoint", mnt, img).CombinedOutput(); err != nil { //nolint:gosec // G204: hdiutil by its full path, on the test's own folder
		t.Fatalf("hdiutil attach: %v: %s", err, out)
	}
	t.Cleanup(func() {
		if out, err := exec.Command("/usr/bin/hdiutil", "detach", "-force", mnt).CombinedOutput(); err != nil { //nolint:noctx,gosec // the test's context has ended by its cleanup; G204: hdiutil by its full path, on the test's own folder
			t.Errorf("the image's cleanup, not the rule: hdiutil detach of %s: %v: %s", mnt, err, out)
		}
	})
	var st unix.Statfs_t
	if err := unix.Statfs(mnt, &st); err != nil || unix.ByteSliceToString(st.Fstypename[:]) != "hfs" || unix.ByteSliceToString(st.Mntonname[:]) != mnt {
		t.Fatalf("the image on %s: type %q on %q, %v, and want hfs mounted on it", mnt, unix.ByteSliceToString(st.Fstypename[:]), unix.ByteSliceToString(st.Mntonname[:]), err)
	}
	return mnt
}

// What it costs when this fails: on an HFS+ volume, as a Time Machine disk is, a folder with a
// second name answers, and so does every name beneath it, by whichever of the folder's names any
// process looked at last, in another folder than the path's: a journal keys one file by two names.
func TestOnMacOSFinalRefusesEveryNameOnAnHFSPlusVolume(t *testing.T) {
	t.Parallel()
	mnt := hfsVolume(t, finalTemp(t))
	x, y := filepath.Join(mnt, "d1", "x"), filepath.Join(mnt, "d2", "y")
	for _, dir := range []string{x, filepath.Dir(y)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(mnt, "file")
	for _, f := range []string{file, filepath.Join(x, "inside")} {
		if err := os.WriteFile(f, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// HFS+ gives a folder a second name by link(2), as Time Machine does.
	if err := unix.Link(x, y); err != nil {
		t.Fatalf("link of the folder %s as %s on HFS+: %v", x, y, err)
	}
	// Each name, by Final, and by final with the folder's other name looked at between the open and
	// the read, as any process may look at it, after which F_GETPATH answers by that name.
	for _, row := range []struct{ p, other string }{{mnt, y}, {file, y}, {x, y}, {filepath.Join(x, "inside"), y}, {y, x}} {
		if got, err := Final(row.p); !errors.Is(err, errors.ErrUnsupported) || got != "" {
			t.Errorf("Final on HFS+ of %s = %q, %v, and want errors.ErrUnsupported", row.p, got, err)
		}
		lookThenRead := func(f *os.File) (string, error) {
			if _, err := os.Lstat(row.other); err != nil {
				return "", err
			}
			return finalOf(f)
		}
		if got, err := final(row.p, lookThenRead, os.Lstat); !errors.Is(err, errors.ErrUnsupported) || got != "" {
			t.Errorf("Final on HFS+ of %s, while %s is looked at = %q, %v, and want errors.ErrUnsupported", row.p, row.other, got, err)
		}
	}
}

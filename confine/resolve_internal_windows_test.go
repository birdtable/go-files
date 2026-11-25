package confine

import (
	"errors"
	"io/fs"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// A target on a drive with no root (C:x) is refused, matching ErrOutside: it leads to that drive's
// working folder, which this process may move.
//
// What it costs when this fails: a link to C:x is answered against the folder the walk reached, and
// the system follows it to another folder altogether.
func TestOnWindowsADriveWithNoRootIsRefused(t *testing.T) {
	t.Parallel()
	_, _, err := hop(`C:\tree`, `C:\tree\l`, `C:x`, nil)
	if !errors.Is(err, ErrOutside) {
		t.Errorf(`hop to C:x = %v, want a refusal matching ErrOutside`, err)
	}
	done, todo, err := hop(`C:\tree`, `C:\tree\l`, `\out\x`, []string{"y"})
	if err != nil || done != `C:\` || len(todo) != 3 || todo[0] != "out" || todo[2] != "y" {
		t.Errorf(`hop to \out\x = %q, %q, %v, want C:\ then out, x, y`, done, todo, err)
	}
}

// An absolute target with a . or a .. among its names is refused, matching ErrOutside; one with
// none passes, a trailing separator included.
//
// What it costs when this fails: the walk takes a .. the system opens as written, and answers a
// folder the system never reaches.
func TestOnWindowsAnAbsoluteTargetWithADotDotIsRefused(t *testing.T) {
	t.Parallel()
	for _, target := range []string{`C:\a\..\b`, `C:\a\.\b`, `\\host\share\..\b`} {
		if err := cleanAbs("l", target); !errors.Is(err, ErrOutside) {
			t.Errorf("cleanAbs(%q) = %v, want a refusal matching ErrOutside", target, err)
		}
	}
	for _, target := range []string{`C:\a\b`, `C:\a\b\`, `C:\`, `\\host\share\b`} {
		if err := cleanAbs("l", target); err != nil {
			t.Errorf("cleanAbs(%q) = %v, want nil", target, err)
		}
	}
}

// A target past \\?\ is kept as a folder when it is a whole volume's root and the mount manager
// lists the link's own path among that volume's paths, as for the system drive's own root, and
// refused, matching ErrOutside, otherwise: a volume's name not mounted at the link, another \\?\
// name, and any \\.\ name. Any other target is followed.
//
// What it costs when this fails: a mount point is answered as \\?\Volume{...}\, which no path
// through a drive compares with, or a junction made by hand to the name of the volume a tree is on
// is kept as a folder inside the tree, and every file on the volume is judged inside it, or a link to \\?\GLOBALROOT\... or \\.\... is followed
// into a device.
func TestOnWindowsAVolumeMountIsKeptAndAnotherDevicePathRefused(t *testing.T) {
	t.Parallel()
	drive := filepath.VolumeName(t.TempDir())
	vol := volumeOf(t, drive+`\`)
	for target, want := range map[string]bool{`C:\out`: true, `\\host\share\out`: true} {
		if follow, err := farTarget("l", target); err != nil || follow != want {
			t.Errorf("farTarget(%q) = %v, %v, want %v, nil", target, follow, err, want)
		}
	}
	if follow, err := farTarget(drive, vol); err != nil || follow {
		t.Errorf("farTarget(%s, %q), the volume mounted there = %v, %v, want false, nil", drive, vol, follow, err)
	}
	for _, target := range []string{
		vol, vol + "sub", `\\?\Volume{0b1c2d3e-4f50-6172-8394-a5b6c7d8e9f0}\`,
		`\\?\GLOBALROOT\Device\HarddiskVolume3\x`, `\\?\C:\x`,
		`\\.\C:\x`, `\\.\GLOBALROOT\Device\HarddiskVolume3\x`, `\\.\` + vol[len(`\\?\`):],
	} {
		if _, err := farTarget(filepath.Join(t.TempDir(), "l"), target); !errors.Is(err, ErrOutside) {
			t.Errorf("farTarget(%q) at a folder that is not its mount point = %v, want a refusal matching ErrOutside", target, err)
		}
	}
}

// volumeOf is the \\?\Volume{...}\ name of the volume mounted at top.
func volumeOf(t *testing.T, top string) string {
	t.Helper()
	p, err := windows.UTF16PtrFromString(top)
	if err != nil {
		t.Fatal(err)
	}
	var name [50]uint16
	if err := windows.GetVolumeNameForVolumeMountPoint(p, &name[0], uint32(len(name))); err != nil {
		t.Fatalf("the volume mounted at %s: %v", top, err)
	}
	return windows.UTF16ToString(name[:])
}

// irregular is a name Go reports as irregular, as it reports a reparse point that is not a link.
type irregular struct{ fs.FileInfo }

func (irregular) Mode() fs.FileMode  { return fs.ModeIrregular | fs.ModeDir }
func (irregular) Name() string       { return "r" }
func (irregular) Size() int64        { return 0 }
func (irregular) ModTime() time.Time { return time.Time{} }
func (irregular) IsDir() bool        { return true }
func (irregular) Sys() any           { return nil }

// A reparse point Go reports as irregular that os.Readlink does not read, as one that is neither
// a link nor a junction, is refused, matching ErrOutside, and never walked through as a folder.
//
// What it costs when this fails: a reparse point the system follows somewhere the walk never
// reads is answered as a plain folder, and a path through it is judged inside.
func TestOnWindowsAnotherReparsePointIsRefused(t *testing.T) {
	t.Parallel()
	_, follow, err := readLink(t.TempDir(), irregular{})
	if !errors.Is(err, ErrOutside) || follow {
		t.Errorf("readLink of an irregular name Readlink cannot read = %v, %v, want a refusal matching ErrOutside", follow, err)
	}
}

// The refusal of a reparse point os.Readlink cannot read carries the system's error as text, so it
// matches ErrOutside and never fs.ErrNotExist, which os.Readlink answers for a tag that is neither
// a link nor a junction.
//
// What it costs when this fails: a caller that asks for fs.ErrNotExist first, as it does for a ..
// after a missing name, takes a reparse point the walk must not pass for a name that is not there.
func TestOnWindowsAnotherReparsePointIsNeverNotExist(t *testing.T) {
	t.Parallel()
	err := otherReparse(opResolve, "r", syscall.ENOENT)
	if !errors.Is(err, ErrOutside) || errors.Is(err, fs.ErrNotExist) {
		t.Errorf("otherReparse(r, ENOENT) = %v, want a refusal matching ErrOutside and not fs.ErrNotExist", err)
	}
}

// A target with a name that ends in a dot or a space is caught, and . and .. are not such names.
//
// What it costs when this fails: the walk's look strips the dot, finds the folder without it, and
// answers a missing name below it, while the system takes the link to another folder.
func TestOnWindowsATrailedTargetIsCaught(t *testing.T) {
	t.Parallel()
	for target, want := range map[string]bool{
		`C:\work\tree.\new`: true, `C:\work\tree \new`: true, `x.`: true, `..\a.\b`: true,
		`C:\work\tree\new`: false, `..\..\x`: false, `.\x`: false, `\\?\Volume{0b1c2d3e-4f50-6172-8394-a5b6c7d8e9f0}\`: false,
	} {
		if got := trailedTarget(target); got != want {
			t.Errorf("trailedTarget(%q) = %v, want %v", target, got, want)
		}
	}
}

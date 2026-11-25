package confine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/birdtable/go-files/confine"
	"golang.org/x/sys/windows"
)

// On Windows LinkFolder makes a junction, a reparse point Go reports as irregular, not a symbolic
// link, so it needs no privilege, and the target's file reads through it.
//
// What it costs when this fails: the link is a symbolic link, which an account without developer
// mode or elevation cannot make, or a plain folder that leads nowhere.
func TestOnWindowsLinkFolderMakesAJunction(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	target := outsideFolder(t)
	if err := confine.LinkFolder(root, target, filepath.Join("deep", "link")); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(dir, "deep", "link")
	if info, err := os.Lstat(full); err != nil || info.Mode()&fs.ModeIrregular == 0 || info.Mode()&fs.ModeSymlink != 0 {
		t.Fatalf("deep/link is %v, %v, want a junction", info, err)
	}
	if got, err := os.Readlink(full); err != nil || got != target {
		t.Errorf("deep/link leads to %q, %v, want %q", got, err, target)
	}
	if got, err := os.ReadFile(filepath.Join(full, "inside.txt")); err != nil || string(got) != "inside\n" {
		t.Errorf("inside.txt through deep/link holds %q, %v", got, err)
	}
}

// A junction at a component of at's folder is refused, matching ErrOutside and naming it, and
// nothing is made where it leads, inside the root or out of it. A component of at that ends in a
// dot or a space is refused the same way, before any call.
//
// What it costs when this fails: any account plants a junction in a folder a program links into,
// with no privilege, and the new link lands where the junction leads.
func TestOnWindowsLinkFolderRefusesAJunctionOnTheWay(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	target := outsideFolder(t)
	out := t.TempDir()
	plantJunction(t, filepath.Join(dir, "real"), filepath.Join(dir, "state"))
	plantJunction(t, out, filepath.Join(dir, "deep", "out"))
	refusedAt(t, `LinkFolder(state\link)`, confine.LinkFolder(root, target, filepath.Join("state", "link")), "state")
	refusedAt(t, `LinkFolder(deep\out\link)`, confine.LinkFolder(root, target, filepath.Join("deep", "out", "link")), "deep/out")
	for _, at := range []string{"link.", "link ", filepath.Join("deep.", "link")} {
		if err := confine.LinkFolder(root, target, at); !errors.Is(err, confine.ErrOutside) {
			t.Errorf("LinkFolder(%q) = %v, want ErrOutside", at, err)
		}
	}
	unchanged(t, dir)
	empty(t, target)
	for _, gone := range []string{filepath.Join(dir, "real", "link"), filepath.Join(out, "link"), filepath.Join(dir, "link")} {
		if _, err := os.Lstat(gone); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s is there: %v", gone, err)
		}
	}
}

// When the system refuses the mount point, the folder LinkFolder made for it, there when the set
// runs, is removed, and the refusal is the answer; the target is left as it was.
//
// What it costs when this fails: a refused link leaves an empty folder at its name, which a later
// LinkFolder refuses as taken and a program reads as the folder it linked.
func TestOnWindowsAHalfMadeJunctionIsRemoved(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	target := outsideFolder(t)
	refused := errors.New("the mount point was refused")
	for _, at := range []string{"link", filepath.Join("deep", "link")} {
		made := false
		err := confine.LinkFolderSet(root, target, at, func() error {
			info, err := os.Lstat(filepath.Join(dir, at))
			made = err == nil && info.IsDir()
			return refused
		})
		if !errors.Is(err, refused) {
			t.Errorf("LinkFolder(%s) = %v, want the set's refusal", at, err)
		}
		if !made {
			t.Errorf("no folder was at %s when the set ran", at)
		}
		if _, err := os.Lstat(filepath.Join(dir, at)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s is left there: %v", at, err)
		}
	}
	unchanged(t, dir)
	empty(t, target)
}

// A target on another machine, a share, one whose mount
// point data would not fit, and one with a component ending in a dot or a space, whose folder
// is there by the Win32 name rules, are refused with fs.ErrInvalid before the system is asked
// whether they are there, and nothing is made.
//
// What it costs when this fails: a junction to a share, which the system does not follow; a
// folder made and then left half made when the system refuses data too long; or a junction to
// real. that leads to nothing, or to a folder of that name made later, while real was checked.
func TestOnWindowsATargetNoJunctionReachesIsRefused(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	real := filepath.Join(dir, "real")
	refused := []string{`\\server\share\folder`, `\\?\` + real, filepath.Join(dir, strings.Repeat("a", 9000)),
		real + ".", real + " ", filepath.Join(real+".", "x")}
	for _, target := range refused {
		err := confine.LinkFolder(root, target, "link")
		if _, ok := errors.AsType[*os.LinkError](err); !ok || !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("LinkFolder(%.40q...) = %v, want an *os.LinkError matching fs.ErrInvalid", target, err)
		}
		if _, err := os.Lstat(filepath.Join(dir, "link")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("link is there: %v", err)
		}
	}
}

// freeDrive is a drive letter no disk is on now, from high down to low. Each test that names a
// letter searches a range of its own, so two running at once never take one letter.
func freeDrive(t *testing.T, high, low rune) string {
	t.Helper()
	used, err := windows.GetLogicalDrives()
	if err != nil {
		t.Fatal(err)
	}
	for c := high; c >= low; c-- {
		if used&(1<<(c-'A')) == 0 {
			return string(c) + ":"
		}
	}
	t.Fatalf("no drive letter from %c down to %c is free", high, low)
	return ""
}

// A target on a drive letter with no disk, and one on a letter made by subst, which names a
// folder in this logon session only, are refused with fs.ErrInvalid, the subst one though the
// folder it names is there, and nothing is made. The letter is a free one, named for the test
// and removed after it, from Z to S, a range no other test searches.
//
// What it costs when this fails: a junction that holds \??\X:\..., which another logon session,
// a service among them, resolves to nothing, or to a folder its own X: names.
func TestOnWindowsATargetOnASubstDriveIsRefused(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	letter := freeDrive(t, 'Z', 'S')
	err := confine.LinkFolder(root, letter+`\folder`, "link")
	if _, ok := errors.AsType[*os.LinkError](err); !ok || !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("LinkFolder(%s\\folder), no disk there, = %v, want an *os.LinkError matching fs.ErrInvalid", letter, err)
	}
	name, err := windows.UTF16PtrFromString(letter)
	if err != nil {
		t.Fatal(err)
	}
	to, err := windows.UTF16PtrFromString(outsideFolder(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.DefineDosDevice(0, name, to); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := windows.DefineDosDevice(windows.DDD_REMOVE_DEFINITION, name, to); err != nil {
			t.Error(err)
		}
	})
	if st, err := os.Stat(letter + `\`); err != nil || !st.IsDir() {
		t.Fatalf("%s\\ is %v, %v, want the folder subst named", letter, st, err)
	}
	err = confine.LinkFolder(root, letter+`\`, "link")
	if _, ok := errors.AsType[*os.LinkError](err); !ok || !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("LinkFolder(%s\\) = %v, want an *os.LinkError matching fs.ErrInvalid", letter, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "link")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("link is there: %v", err)
	}
}

// A target whose name holds an unpaired surrogate, which Go holds as WTF-8, gets a junction to
// that folder: the junction holds the name the check read, and the target's file reads through.
//
// What it costs when this fails: the junction holds U+FFFD where the surrogate was, a name the
// check never looked at, which leads to nothing or to a folder of that name made later.
func TestOnWindowsAJunctionHoldsAnUnpairedSurrogate(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	target := filepath.Join(t.TempDir(), "a\xed\xa0\x80b")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "inside.txt"), []byte("inside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := confine.LinkFolder(root, target, "link"); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(dir, "link")
	if got, err := os.Readlink(full); err != nil || got != target {
		t.Errorf("link leads to %q, %v, want %q", got, err, target)
	}
	if got, err := os.ReadFile(filepath.Join(full, "inside.txt")); err != nil || string(got) != "inside\n" {
		t.Errorf("inside.txt through link holds %q, %v", got, err)
	}
}

// rawLetter is a free letter from high down to low that DefineDosDevice names, with a raw target,
// raw in this logon session, removed after t. Each test that names a letter searches a range of
// its own.
func rawLetter(t *testing.T, raw string, high, low rune) string {
	t.Helper()
	letter := freeDrive(t, high, low)
	name, err := windows.UTF16PtrFromString(letter)
	if err != nil {
		t.Fatal(err)
	}
	to, err := windows.UTF16PtrFromString(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.DefineDosDevice(windows.DDD_RAW_TARGET_PATH, name, to); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := windows.DefineDosDevice(windows.DDD_RAW_TARGET_PATH|windows.DDD_REMOVE_DEFINITION|windows.DDD_EXACT_MATCH_ON_REMOVE, name, to); err != nil {
			t.Error(err)
		}
	})
	return letter
}

// volumeDevice is the device the volume of path is, \Device\HarddiskVolumeN, and path's name
// on it, from its first separator.
func volumeDevice(t *testing.T, path string) (dev, rest string) {
	t.Helper()
	vol := filepath.VolumeName(path)
	from, err := windows.UTF16PtrFromString(vol)
	if err != nil {
		t.Fatal(err)
	}
	var buf [windows.MAX_PATH]uint16
	n, err := windows.QueryDosDevice(from, &buf[0], windows.MAX_PATH)
	if err != nil {
		t.Fatal(err)
	}
	return windows.UTF16ToString(buf[:n]), path[len(vol):]
}

// A target on a letter DefineDosDevice made with a raw target to a folder, a device path that
// names it in this logon session only, is refused with fs.ErrInvalid though the folder is there
// and the letter names a device, not a \??\ name as subst's does, and nothing is made. The letter
// is a free one from R to L, a range no other test searches.
//
// What it costs when this fails: a junction that holds \??\X:\..., which another logon session,
// a service among them, resolves to nothing, or to a folder its own X: names.
func TestOnWindowsATargetOnARawTargetDriveIsRefused(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	target := outsideFolder(t)
	dev, rest := volumeDevice(t, target)
	letter := rawLetter(t, dev+rest, 'R', 'L')
	if _, err := os.Stat(letter + `\inside.txt`); err != nil {
		t.Fatalf("%s\\inside.txt: %v, want the file of the folder %s names", letter, err, dev+rest)
	}
	err := confine.LinkFolder(root, letter+`\`, "link")
	if _, ok := errors.AsType[*os.LinkError](err); !ok || !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("LinkFolder(%s\\), a raw target to %s, = %v, want an *os.LinkError matching fs.ErrInvalid", letter, dev+rest, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "link")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("link is there: %v", err)
	}
	empty(t, target)
}

// A target on a letter DefineDosDevice made with a raw target to a whole volume is refused with
// fs.ErrInvalid, though the mount manager names a volume at its root, which it resolves to the
// volume's device: the letter is not among the paths the mount manager mounted that volume at,
// and nothing is made. The letter is a free one from K to D, a range no other test searches.
//
// What it costs when this fails: a junction that holds \??\X:\..., which another logon session,
// a service among them, resolves to nothing, or to a folder its own X: names.
func TestOnWindowsATargetOnARawLetterToAWholeVolumeIsRefused(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	target := outsideFolder(t)
	dev, rest := volumeDevice(t, target)
	letter := rawLetter(t, dev, 'K', 'D')
	if _, err := os.Stat(letter + rest + `\inside.txt`); err != nil {
		t.Fatalf("%s%s\\inside.txt: %v, want the file the volume %s holds there", letter, rest, err, dev)
	}
	err := confine.LinkFolder(root, letter+rest, "link")
	if _, ok := errors.AsType[*os.LinkError](err); !ok || !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("LinkFolder(%s%s), a raw target to the volume %s, = %v, want an *os.LinkError matching fs.ErrInvalid", letter, rest, dev, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "link")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("link is there: %v", err)
	}
	empty(t, target)
}

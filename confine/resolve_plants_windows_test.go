package confine_test

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/birdtable/go-files/confine"
)

// refusedOutside holds Resolve(path) and Below(folder, path) to refusals matching ErrOutside.
func refusedOutside(t *testing.T, what, folder, path string) {
	t.Helper()
	if got, err := confine.Resolve(path); !errors.Is(err, confine.ErrOutside) {
		t.Errorf("Resolve(%s) with %s = %q, %v, want a refusal matching ErrOutside", path, what, got, err)
	}
	if got, err := confine.Below(folder, path); !errors.Is(err, confine.ErrOutside) {
		t.Errorf("Below(%s, %s) with %s = %q, %v, want a refusal matching ErrOutside", folder, path, what, got, err)
	}
}

// A junction made by hand to the name of the volume the tree is on, which takes no privilege, is
// refused: the mount manager does not list it among the volume's paths, as it lists a mount
// point.
//
// What it costs when this fails: the junction is kept as a folder inside the tree, and every file
// on the volume, C:\Users\x by tree\j\Users\x, is judged inside it.
func TestOnWindowsAJunctionToTheTreesVolumeIsRefused(t *testing.T) {
	t.Parallel()
	base := resolvedTemp(t)
	tree(t, base)
	top := filepath.VolumeName(base) + `\`
	p, err := windows.UTF16PtrFromString(top)
	if err != nil {
		t.Fatal(err)
	}
	var name [50]uint16
	if err := windows.GetVolumeNameForVolumeMountPoint(p, &name[0], uint32(len(name))); err != nil {
		t.Fatalf("the volume mounted at %s: %v", top, err)
	}
	vol := windows.UTF16ToString(name[:]) // \\?\Volume{...}\
	if err := confine.PlantRawJunction(at(base, "tree", "j"), `\??\`+vol[len(`\\?\`):], vol); err != nil {
		t.Fatalf("a junction to %s: %v", vol, err)
	}
	refusedOutside(t, "j a junction to "+vol, at(base, "tree"), at(base, "tree", "j", base[len(top):], "out", "x"))
}

// A junction whose target has a name that ends in a dot is refused, though the walk's look by the
// Win32 rules finds the folder without the dot.
//
// What it costs when this fails: tree\j, a junction to tree.\new, is answered tree\new, a missing
// name inside the tree, while the system takes it to a folder tree. beside the tree.
func TestOnWindowsAJunctionToATrailedNameIsRefused(t *testing.T) {
	t.Parallel()
	base := resolvedTemp(t)
	tree(t, base)
	target := at(base, "tree") + `.\new`
	if err := confine.PlantRawJunction(at(base, "tree", "j"), `\??\`+target, target); err != nil {
		t.Fatalf("a junction to %s: %v", target, err)
	}
	refusedOutside(t, "j a junction to "+target, at(base, "tree"), at(base, "tree", "j"))
}

// A junction whose target is an NT name with no drive, which os.Readlink gives as written, is
// refused, where the system lets one be made.
//
// What it costs when this fails: \Device\HarddiskVolume3\out\new is rooted on the link's own
// volume, and a missing name there is answered as C:\Device\..., a folder the system never
// reaches.
func TestOnWindowsAJunctionToAnNTNameIsRefused(t *testing.T) {
	t.Parallel()
	base := resolvedTemp(t)
	tree(t, base)
	for i, sub := range []string{`\Device\HarddiskVolume3\out\new`, `\SystemRoot\new`, `\DosDevices\C:\out\new`} {
		j := at(base, "tree", "j"+string(rune('0'+i)))
		if err := confine.PlantRawJunction(j, sub, sub); err != nil {
			t.Logf("the system refuses a junction to %s: %v", sub, err)
			continue
		}
		refusedOutside(t, "a junction to "+sub, at(base, "tree"), at(j, "x"))
	}
}

// A reparse point that is neither a link nor a junction, which os.Readlink answers with ENOENT,
// is refused matching ErrOutside and never fs.ErrNotExist, where the system lets one be made.
//
// What it costs when this fails: a caller that asks for fs.ErrNotExist first takes a reparse point
// the walk must not pass for a name that is not there.
func TestOnWindowsAnUnreadReparsePointIsNeverNotExist(t *testing.T) {
	t.Parallel()
	base := resolvedTemp(t)
	tree(t, base)
	r := at(base, "tree", "r")
	if err := confine.PlantOtherReparse(r); err != nil {
		t.Logf("the system refuses a WSL link at %s, so the walk's refusal of one is held by TestOnWindowsAnotherReparsePointIsNeverNotExist alone: %v", r, err)
		return
	}
	_, err := confine.Resolve(at(r, "x"))
	if !errors.Is(err, confine.ErrOutside) || errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Resolve(tree/r/x) with r a WSL link = %v, want a refusal matching ErrOutside and not fs.ErrNotExist", err)
	}
}

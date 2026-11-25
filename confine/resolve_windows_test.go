package confine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// A junction is followed as a link is, which filepath.EvalSymlinks has not done since Go 1.23,
// and Below finds a path through a junction out of the tree outside it.
//
// What it costs when this fails: a junction out of a node_modules tree resolves to its own name,
// and a copy judges it inside and follows it out.
func TestOnWindowsResolveFollowsAJunction(t *testing.T) {
	t.Parallel()
	base := resolvedTemp(t)
	tree(t, base)
	plantJunction(t, at(base, "out"), at(base, "tree", "j"))
	resolvesTo(t, at(base, "tree", "j", "x"), at(base, "out", "x"))
	if got, err := confine.Below(at(base, "tree"), at(base, "tree", "j", "x")); err == nil {
		t.Errorf("Below(tree, tree/j/x) with j a junction out = %q, want a refusal", got)
	}
	plantJunction(t, at(base, "tree", "sub"), at(base, "out", "in"))
	if got, err := confine.Below(at(base, "tree"), at(base, "out", "in")); err != nil || got != "sub" {
		t.Errorf("Below(tree, out/in) with in a junction into tree = %q, %v, want sub", got, err)
	}
}

// A path given with a .. after a junction or a link out of the tree is taken by its spelling, as
// Windows takes it before it opens anything: tree\junc\..\x and tree\link\..\x are tree\x, the
// file os.Stat finds, and Below names it x.
//
// What it costs when this fails: the walk goes up from the junction's target while the system goes
// by the spelling, and a path the system opens inside the tree is refused as changed, or answered
// as the file beside the target.
func TestOnWindowsADotDotInAPathIsTakenByItsSpelling(t *testing.T) {
	t.Parallel()
	base := resolvedTemp(t)
	tree(t, base)
	in := at(base, "tree")
	plantJunction(t, at(base, "out", "sub"), at(in, "junc"))
	plantLink(t, at(base, "out", "sub"), at(in, "link"))
	for _, via := range []string{"junc", "link"} {
		path := raw(in, via, "..", "x")
		resolvesTo(t, path, at(in, "x"))
		sys, sysErr := os.Stat(path)
		ans, ansErr := os.Stat(at(in, "x"))
		if sysErr != nil || ansErr != nil || !os.SameFile(sys, ans) {
			t.Errorf("os.Stat(tree\\%s\\..\\x) is not tree\\x (%v, %v)", via, sysErr, ansErr)
		}
		if got, err := confine.Below(in, path); err != nil || got != "x" {
			t.Errorf("Below(tree, tree\\%s\\..\\x) = %q, %v, want x", via, got, err)
		}
	}
}

// A verbatim path (\\?\) with a . or a .. past its volume is refused by both, matching
// fs.ErrInvalid and ErrOutside: past \\?\ the system hands each name to the file system as
// written, and takes no .. by its spelling.
//
// What it costs when this fails: \\?\C:\tree\junc\..\x is walked as C:\tree\x, and where the tail
// is missing, Below answers a name in the folder for a path the system takes elsewhere.
func TestOnWindowsAnUncleanVerbatimPathIsRefused(t *testing.T) {
	t.Parallel()
	devicePathRefused(t, `\\?\`)
}

// A device path (\\.\) with a . or a .. past its volume is refused by both, matching fs.ErrInvalid
// and ErrOutside: the system takes the first name past \\.\ for no part of the volume, as Go's
// Clean does, so \\.\UNC\host\share\..\other is \\host\other to it.
//
// What it costs when this fails: a path the system takes to another share or device is walked as
// one inside the folder, and Below answers a name in it.
func TestOnWindowsAnUncleanDotDevicePathIsRefused(t *testing.T) {
	t.Parallel()
	devicePathRefused(t, `\\.\`)
}

func devicePathRefused(t *testing.T, prefix string) {
	t.Helper()
	base := resolvedTemp(t)
	tree(t, base)
	in := at(base, "tree")
	plantJunction(t, at(base, "out", "sub"), at(in, "junc"))
	for _, tail := range [][]string{{"junc", "..", "x"}, {"gone", "..", "y"}, {".", "x"}} {
		path := prefix + raw(append([]string{in}, tail...)...)
		if got, err := confine.Resolve(path); !errors.Is(err, fs.ErrInvalid) || !errors.Is(err, confine.ErrOutside) {
			t.Errorf("Resolve(%s) = %q, %v, want fs.ErrInvalid and ErrOutside", path, got, err)
		}
		if got, err := confine.Below(in, path); !errors.Is(err, fs.ErrInvalid) || !errors.Is(err, confine.ErrOutside) {
			t.Errorf("Below(tree, %s) = %q, %v, want fs.ErrInvalid and ErrOutside", path, got, err)
		}
	}
}

// A link target rooted with no drive (\x) is on the link's own volume.
//
// What it costs when this fails: \x is joined under the link's folder, and the answer names a
// folder the system never reaches.
func TestOnWindowsARootedTargetIsOnTheLinksVolume(t *testing.T) {
	t.Parallel()
	base := resolvedTemp(t)
	tree(t, base)
	rooted := at(base, "out")[len(filepath.VolumeName(base)):]
	plantLink(t, rooted, at(base, "tree", "r"))
	if got, err := os.Readlink(at(base, "tree", "r")); err != nil || got != rooted {
		t.Fatalf("the planted link reads %q, %v, want %q", got, err, rooted)
	}
	resolvesTo(t, at(base, "tree", "r", "x"), at(base, "out", "x"))
}

// Below compares by file ID: a folder's 8.3 short name, where the volume makes one, names the
// same folder as its long name.
//
// What it costs when this fails: a temp folder spelled RUNNER~1 refuses every path spelled
// runneradmin below it.
func TestOnWindowsBelowTakesAShortNameAsTheFolder(t *testing.T) {
	t.Parallel()
	base := resolvedTemp(t)
	long := at(base, "a folder with a long name")
	if err := os.MkdirAll(at(long, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	short := shortName(t, long)
	if got, err := confine.Below(short, at(long, "sub")); err != nil || got != "sub" {
		t.Errorf("Below(%s, %s) = %q, %v, want sub", short, at(long, "sub"), got, err)
	}
	if got, err := confine.Below(long, at(short, "sub")); err != nil || got != "sub" {
		t.Errorf("Below(%s, %s) = %q, %v, want sub", long, at(short, "sub"), got, err)
	}
}

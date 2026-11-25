package treesum

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The chain tree: the folder d, depth folders deep, and the links c0 to c9 at the top, each
// down through every d and back up to the next, the last to d; and empty folders, so every
// depth up to deepest makes a tree of the same count of entries. Ten links, not hops, keep the
// tree inside the budget at depth 1 under any charge, and above it at deepest only where each
// component is charged the folders of its path.
const deepest, chained = 100, 10

func chain(t *testing.T, depth int) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, strings.Repeat("d"+string(filepath.Separator), depth)), 0o755); err != nil {
		t.Fatal(err)
	}
	var links [][2]string
	for i := range chained {
		links = append(links, [2]string{fmt.Sprintf("c%d", i), strings.Repeat("d/", depth) + strings.Repeat("../", depth) + fmt.Sprintf("c%d", i+1)})
	}
	links[chained-1][1] = "d"
	for _, l := range links {
		if err := os.Symlink(filepath.FromSlash(l[1]), filepath.Join(dir, l[0])); err != nil {
			t.Fatal(err)
		}
	}
	for i := depth; i < deepest; i++ {
		if err := os.Mkdir(filepath.Join(dir, fmt.Sprintf("p%d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// What it costs when this fails: a tree well inside its caps, of links that go deep and back
// through each other, makes the walk run as long as its maker likes, which Caps says it cannot.
func TestTheLinksTakeNoMoreStepsThanTheCapsAllowAtAnyDepth(t *testing.T) {
	t.Parallel()
	const entries = deepest + chained
	var deep string
	for _, depth := range []int{1, deepest} {
		deep = chain(t, depth)
		root, err := os.OpenRoot(deep)
		if err != nil {
			t.Fatal(err)
		}
		w := &walk{entries: entries, bytes: 0, steps: budget(entries), kinds: map[string]entry{}, reached: func(string, string) {}}
		if err := w.folder(root, "."); err != nil {
			t.Fatal(err)
		}
		err = w.links()
		if depth == 1 && err != nil {
			t.Errorf("at depth 1 the chain is refused: %v", err)
		}
		if depth == deepest && !errors.Is(err, ErrCap) {
			t.Errorf("at depth %d the links gave %v, and want ErrCap", depth, err)
		}
		_ = root.Close()
	}
	if _, _, err := Sum(rootOf(t, deep), ".", Caps{Entries: entries, Bytes: 0}); !errors.Is(err, ErrCap) {
		t.Errorf("Sum of the chain %d deep gave %v, and want ErrCap", deepest, err)
	}
}

func rootOf(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

// What it costs when this fails: links whose targets run on past a name that is not a folder,
// components no resolution walks, are split and scanned at every link that leads to them, and
// cost the walk far more than the caps charge.
func TestTheComponentsALinkDoesNotWalkAreChargedToo(t *testing.T) {
	t.Parallel()
	for _, past := range []int{1, 500} {
		dir := t.TempDir()
		for i := range chained {
			target := fmt.Sprintf("c%d", i+1) + strings.Repeat("/x", past)
			if i == chained-1 {
				target = "gone"
			}
			if err := os.Symlink(filepath.FromSlash(target), filepath.Join(dir, fmt.Sprintf("c%d", i))); err != nil {
				t.Fatal(err)
			}
		}
		_, _, err := Sum(rootOf(t, dir), ".", Caps{Entries: chained, Bytes: 0})
		if past == 1 && err != nil {
			t.Errorf("with 1 component past each link the chain is refused: %v", err)
		}
		if past == 500 && !errors.Is(err, ErrCap) {
			t.Errorf("with %d components past each link Sum gave %v, and want ErrCap", past, err)
		}
	}
}

// What it costs when this fails: a refusal names one link on one file system and another link on
// the next, as a folder read on tmpfs gives its names newest first and on APFS by name, so the
// same tree is refused in two ways.
func TestALinkRefusalNamesTheSameLinkInAnyReadOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, l := range [][2]string{{"a", "b"}, {"b", "a"}} {
		if err := os.Symlink(l[1], filepath.Join(dir, l[0])); err != nil {
			t.Fatal(err)
		}
	}
	for _, reversed := range []bool{false, true} {
		w := &walk{entries: 2, bytes: 0, steps: budget(2), kinds: map[string]entry{}, reached: func(string, string) {}}
		if err := w.folder(rootOf(t, dir), "."); err != nil {
			t.Fatal(err)
		}
		if reversed {
			slices.Reverse(w.found)
		}
		if pe, ok := errors.AsType[*fs.PathError](w.links()); !ok || pe.Path != "a" {
			t.Errorf("with the folder read reversed %v the loop's refusal is %v, and want it to name a", reversed, pe)
		}
	}
}

// What it costs when this fails: a link whose target runs out of steps, or loops, and then climbs
// with a `..` is refused as leading out of the tree, and the caller is told the wrong reason.
func TestARefusalFurtherAlongIsKeptPastAClimb(t *testing.T) {
	t.Parallel()
	w := &walk{steps: 5, kinds: map[string]entry{"x": {kind: "link", path: "x", text: "x/x/x/x/x/x/x/x"}}}
	if err := w.inside(entry{kind: "link", path: "l", text: "x/.."}); !errors.Is(err, ErrCap) {
		t.Errorf("a link through one out of steps and then up gave %v, and want ErrCap", err)
	}
}

// nested makes a chain of n folders named name, each inside the last, in a new folder, by
// handle, since a path that long is past what some systems open by name; it gives the root.
func nested(t *testing.T, n int, name string) *os.Root {
	t.Helper()
	top := rootOf(t, t.TempDir())
	at := top
	for range n {
		err := at.Mkdir(name, 0o755)
		var sub *os.Root
		if err == nil {
			sub, err = at.OpenRoot(name)
		}
		if at != top {
			err = errors.Join(err, at.Close())
		}
		if err != nil {
			t.Fatal(err)
		}
		at = sub
	}
	if at != top {
		_ = at.Close()
	}
	return top
}

// What it costs when this fails: a chain of folders inside the caps makes the listing, and what
// Sum allocates, grow with the square of its depth, and the process that checks it ends.
func TestAPathLongerThanTheLongestLinuxOpensIsRefusedAsACap(t *testing.T) {
	t.Parallel()
	name := strings.Repeat("n", 255)
	// 16 names and their 15 slashes are 4,095 bytes, the longest path taken.
	if _, _, err := Sum(nested(t, 16, name), ".", Caps{Entries: 17}); err != nil {
		t.Errorf("a chain whose deepest path is 4,095 bytes gave %v", err)
	}
	_, _, err := Sum(nested(t, 17, name), ".", Caps{Entries: 17})
	if pe, ok := errors.AsType[*fs.PathError](err); !errors.Is(err, ErrCap) || !ok || len(pe.Path) != 4351 {
		t.Errorf("a chain whose deepest path is 4,351 bytes gave %v, and want ErrCap naming it", err)
	}
}

// What it costs when this fails: the walk holds a folder open for every level above the one it
// reads, so a chain deeper than the host has handles ends the walk with no ErrCap and leaves the
// process no handle for anything else.
func TestTheFoldersHeldOpenStopAtTheDepthTheLongestPathAllows(t *testing.T) {
	t.Parallel()
	// 256 names of 15 bytes and their slashes are 4,095 bytes. With names of 1 byte the walk stops
	// at 2,048 folders the same way, but macOS makes and removes each folder of a chain in time that
	// grows with its depth, and a chain of 2,049 takes half a minute.
	deepest := 0 // the walk holds one handle for each folder of the deepest it opens, and the top's
	_, _, err := sum(nested(t, 257, strings.Repeat("n", 15)), ".", Caps{Entries: 300}, func(step, at string) {
		if step == stepFolder {
			deepest = max(deepest, strings.Count(at, "/")+1)
		}
	})
	if !errors.Is(err, ErrCap) || deepest != 256 {
		t.Errorf("a chain of 257 folders gave %v, with %d folders open at the deepest, and want ErrCap at 256", err, deepest)
	}
}

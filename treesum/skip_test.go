package treesum_test

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"testing"

	"github.com/birdtable/go-files/confine"
	"github.com/birdtable/go-files/treesum"
)

// modules is a Skip that leaves out every name node_modules, at any depth, and says what it was asked.
func modules(asked *[]string) func(string) bool {
	return func(rel string) bool {
		*asked = append(*asked, rel)
		return path.Base(rel) == "node_modules"
	}
}

// What it costs when this fails: a caller that leaves a folder out of a pinned tree, such as
// node_modules with its hard-linked store, still has it opened, counted and refused, and cannot pin
// the tree at all, or the digest of what it keeps is not the digest of that tree on its own.
func TestASkippedFolderIsNeverOpenedCountedOrRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plant(t, dir, "src/", "src/a x", "src/node_modules/", "src/node_modules/f more than the caps allow",
		"node_modules/", "node_modules/t/", "node_modules/t/f more than the caps allow", "node_modules/out -> ../../x")
	if err := os.Link(filepath.Join(dir, "node_modules", "t", "f"), filepath.Join(dir, "node_modules", "other")); err != nil {
		t.Fatal(err)
	}
	root := rootAt(t, dir)
	var asked []string
	// Two entries and one byte, which src and src/a take and nothing more.
	skipping := treesum.Caps{Entries: 2, Bytes: 1, Skip: modules(&asked)}
	digest, listing, err := treesum.Sum(root, ".", skipping)
	if err != nil {
		t.Fatalf("Sum with node_modules skipped refused the tree: %v", err)
	}
	wantDigest, wantListing, err := treesum.Sum(tree(t, "src/", "src/a x"), ".", big())
	if err != nil {
		t.Fatal(err)
	}
	if digest != wantDigest || string(listing) != string(wantListing) {
		t.Errorf("Sum with node_modules skipped gave %s and\n%s\nand the tree without it gives %s and\n%s", digest, listing, wantDigest, wantListing)
	}
	slices.Sort(asked)
	if want := []string{"node_modules", "src", "src/a", "src/node_modules"}; !slices.Equal(asked, want) {
		t.Errorf("Skip was asked of %q, and want %q, nothing below a name it skipped", asked, want)
	}
	if err := treesum.Check(root, ".", skipping, digest, listing); err != nil {
		t.Errorf("Check with the same Skip refused the tree Sum gave: %v", err)
	}
	// The same tree with no skip is refused: by the caps, and with caps it fits, by the hard link.
	if _, _, err := treesum.Sum(root, ".", treesum.Caps{Entries: 2, Bytes: 1}); !errors.Is(err, treesum.ErrCap) {
		t.Errorf("Sum with no skip under the same caps gave %v, and want a refusal matching ErrCap", err)
	}
	refused(t, root, "node_modules/other", confine.ErrOutside)
	if err := treesum.Check(root, ".", big(), digest, listing); !errors.Is(err, confine.ErrOutside) {
		t.Errorf("Check with no skip gave %v, and want the hard link refused", err)
	}
}

// What it costs when this fails: a pinned tree holds a link into a folder the caller left out,
// which may itself be a link out of the tree, and the program that runs from the tree reaches what
// the pin never looked at.
func TestALinkIntoASkippedNameIsRefused(t *testing.T) {
	t.Parallel()
	var asked []string
	caps := big()
	caps.Skip = modules(&asked)
	for _, c := range []struct {
		spec []string
		at   string
	}{
		{[]string{"node_modules/", "node_modules/x a", "bin -> node_modules/x"}, "bin"},
		{[]string{"node_modules -> ../outside", "bin -> node_modules/x"}, "bin"},
		{[]string{"node_modules/", "gone -> missing"}, "gone"}, // missing beside a skipped name
		{[]string{"a/", "a/node_modules/", "l -> a/node_modules"}, "l"},
		{[]string{"node_modules/", "d/", "d/up -> ../node_modules/x/.."}, "d/up"},
		// Below a missing name, as a volume that folds case reads A as the kept a.
		{[]string{"a/", "a/node_modules/", "a/node_modules/x s", "b -> A/node_modules/x"}, "b"},
		// The same through a second link, which ends at the missing name.
		{[]string{"a/", "a/node_modules/", "a/node_modules/x s", "c -> A", "b -> c/node_modules/x"}, "b"},
		// The same in the other Unicode form of a kept café, as macOS folds it.
		{[]string{"café/", "café/node_modules/", "b -> café/node_modules/x"}, "b"},
	} {
		digest, _, err := treesum.Sum(tree(t, c.spec...), ".", caps)
		if pe, ok := errors.AsType[*fs.PathError](err); !ok || pe.Path != c.at || !errors.Is(err, confine.ErrOutside) {
			t.Errorf("Sum of %q gave %q, %v, and want %s refused matching confine.ErrOutside", c.spec, digest, err, c.at)
		}
	}
	// A link that reaches a missing name in a folder that skipped nothing stays as it was.
	if _, _, err := treesum.Sum(tree(t, "node_modules/", "src/", "src/l -> missing", "x -> src/l"), ".", caps); err != nil {
		t.Errorf("Sum of a link to a missing name in a folder that skipped nothing gave %v", err)
	}
	// Below a missing name, a link stays inside where nothing was skipped, with or without a Skip.
	for _, c := range []treesum.Caps{caps, big()} {
		if _, _, err := treesum.Sum(tree(t, "a/", "c -> A", "b -> A/x", "d -> c/x"), ".", c); err != nil {
			t.Errorf("Sum, with Skip %t, of links below a missing name in a tree that skipped nothing gave %v", c.Skip != nil, err)
		}
	}
	// With no skip, the link into node_modules is a link that stays inside.
	if _, _, err := treesum.Sum(tree(t, "node_modules/", "node_modules/x a", "bin -> node_modules/x"), ".", big()); err != nil {
		t.Errorf("Sum with no skip of a link into node_modules gave %v", err)
	}
}

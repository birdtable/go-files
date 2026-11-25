//go:build linux || darwin || freebsd

package confine_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// linkedCopy copies a tree holding real/f.txt, with a folder link at sub/up, a file link at file
// and a link to src itself at self, into to, and answers the paths through each link to the copy's
// f.txt.
func linkedCopy(t *testing.T, to string) []string {
	t.Helper()
	from := srcTree(t, "real/f.txt", "sub/g.txt")
	src := rootOf(t, from)
	if err := confine.LinkFolder(src, filepath.Join(from, "real"), filepath.Join("sub", "up")); err != nil {
		t.Fatal(err)
	}
	if err := confine.LinkFile(src, filepath.Join(from, "real", "f.txt"), "file"); err != nil {
		t.Fatal(err)
	}
	if err := confine.LinkFolder(src, from, "self"); err != nil {
		t.Fatal(err)
	}
	if err := confine.CopyTree(rootOf(t, to), src); err != nil {
		t.Fatal(err)
	}
	return []string{filepath.Join("sub", "up", "f.txt"), "file", filepath.Join("self", "real", "f.txt")}
}

// A copy CopyTree made, moved to another name, still reads its own file through each of its
// links, and never what is later made at its old name.
//
// What it costs when this fails: a copied fixture or recorded tree moved into place leads through
// its links back to where it was copied, to nothing or to whatever is made there next.
func TestCopyTreeMakesACopyWhoseLinksHoldWhenItIsMoved(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	to, moved := filepath.Join(base, "copy"), filepath.Join(base, "moved")
	if err := os.Mkdir(to, 0o700); err != nil {
		t.Fatal(err)
	}
	through := linkedCopy(t, to)
	if err := os.Rename(to, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(to, "real"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(to, "real", "f.txt"), []byte("old name"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range through {
		if got, err := os.ReadFile(filepath.Join(moved, p)); err != nil || string(got) != "real/f.txt" {
			t.Errorf("%s in the moved copy reads %q, %v, and want the copy's own file", p, got, err)
		}
	}
}

// An os.Root over a copy CopyTree made reads the copy's file through each of its links, as each
// leads to a name inside the root.
//
// What it costs when this fails: a caller that reads the copy root to root, as this library has it
// read, is refused at every link of the copy as a path that escapes it.
func TestCopyTreeMakesACopyWhoseLinksAnOsRootOverItFollows(t *testing.T) {
	t.Parallel()
	to := t.TempDir()
	through := linkedCopy(t, to)
	r := rootOf(t, to)
	for _, p := range through {
		if got, err := r.ReadFile(p); err != nil || string(got) != "real/f.txt" {
			t.Errorf("%s read through an os.Root over the copy is %q, %v, and want the copy's file", p, got, err)
		}
	}
}

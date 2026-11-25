//go:build unix

package treesum_test

import (
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/birdtable/go-files/confine"
	"github.com/birdtable/go-files/treesum"
)

// What it costs when this fails: a FIFO or a socket in a pinned tree is read, and the walk
// waits on a writer, or the program that runs from the tree talks to what it never pinned.
func TestAFIFOAndASocketAreRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "fifo"), 0o644); err != nil {
		t.Fatal(err)
	}
	refused(t, rootAt(t, dir), "fifo", treesum.ErrKind)
	short, err := os.MkdirTemp("", "s")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	l, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", filepath.Join(short, "sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	refused(t, rootAt(t, short), "sock", treesum.ErrKind)
}

// What it costs when this fails: a tree with two entries it refuses is refused naming one on one
// file system and the other on the next, as APFS reads a folder in its own order, tmpfs newest
// first and ext4 by hash.
func TestARefusalNamesTheFirstEntryByNameInAnyReadOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"m", "q", "b", "z", "a", "k", "c", "y"} {
		if err := syscall.Mkfifo(filepath.Join(dir, name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	refused(t, rootAt(t, dir), "a", treesum.ErrKind)
}

// What it costs when this fails: a name or a link's target that Windows cannot hold, or that
// reads as two paths there, is pinned on Unix, and a line of the listing is read as two.
func TestANameOrATargetNotTheSameOnEverySystemIsRefused(t *testing.T) {
	t.Parallel()
	refused(t, tree(t, "tab\there a"), "tab\there", treesum.ErrKind)
	refused(t, tree(t, "line\nbreak/"), "line\nbreak", treesum.ErrKind)
	refused(t, tree(t, `l -> ..\..\x`), "l", confine.ErrOutside)
	refused(t, tree(t, "l -> a:b"), "l", confine.ErrOutside)
	refused(t, tree(t, "f a", "l -> f/..NamedFork/rsrc"), "l", confine.ErrOutside)
	refused(t, tree(t, "l -> x\ty"), "l", confine.ErrOutside)
}

// What it costs when this fails: a caller handed a FIFO's name for the tree's top waits on its
// open until some process writes to it.
func TestAFIFOAtTheTopIsRefusedWithNoWait(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "fifo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if digest, _, err := treesum.Sum(rootAt(t, dir), "fifo", big()); err == nil {
		t.Fatalf("Sum of a FIFO gave %s", digest)
	}
}

// What it costs when this fails: a caller cannot pin a checkout whose node_modules holds a FIFO or
// a hard-linked file, though it left node_modules out, or a tree it pins with them holds them.
func TestAFIFOAndAHardLinkInASkippedFolderAreNotListed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plant(t, dir, "node_modules/", "node_modules/f a", "src/", "src/a x")
	if err := syscall.Mkfifo(filepath.Join(dir, "node_modules", "fifo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(dir, "node_modules", "f"), filepath.Join(dir, "node_modules", "g")); err != nil {
		t.Fatal(err)
	}
	root := rootAt(t, dir)
	var asked []string
	caps := big()
	caps.Skip = modules(&asked)
	_, listing, err := treesum.Sum(root, ".", caps)
	if want := "folder\tsrc\t\nfile\tsrc/a\t2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881\n"; err != nil || string(listing) != want {
		t.Errorf("Sum with node_modules skipped gave\n%s\n%v, and want\n%s", listing, err, want)
	}
	refused(t, root, "node_modules/f", confine.ErrOutside) // with no skip, the hard link first by name
	if err := os.Remove(filepath.Join(dir, "node_modules", "g")); err != nil {
		t.Fatal(err)
	}
	refused(t, root, "node_modules/fifo", treesum.ErrKind) // and the FIFO behind it
}

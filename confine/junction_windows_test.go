package confine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// plantJunction makes link a junction, a mount point reparse point, to the folder target, which
// any account may make with no privilege.
func plantJunction(t *testing.T, target, link string) {
	t.Helper()
	confine.PlantJunction(t, target, link)
}

// A junction in the middle of a path, which needs no privilege to make, is refused by NoLinks, by
// Open however the file is opened, and by RenameNoReplace at either name, each matching ErrOutside
// and naming the junction; the file it leads to is left as it was, and the same file by its real
// path opens.
//
// What it costs when this fails: any account on Windows plants a junction in a folder a program
// writes, and the write, or the file moved there, lands where the junction leads, outside the root.
func TestOnWindowsAJunctionInTheMiddleIsRefused(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	plantJunction(t, filepath.Join(dir, "real"), filepath.Join(dir, "state"))
	if info, err := os.Lstat(filepath.Join(dir, "state")); err != nil || info.Mode()&fs.ModeIrregular == 0 {
		t.Fatalf("state is %v, %v, want a junction", info, err)
	}
	name := filepath.Join("state", "HEAD")
	_, err := confine.NoLinks(root, name)
	refusedAt(t, "NoLinks(state/HEAD)", err, "state")
	tried := 0
	for how, flag := range opens() {
		f, err := confine.Open(root, name, flag, 0o644)
		if f != nil {
			_ = f.Close()
		}
		refusedAt(t, "Open(state/HEAD) to "+how, err, "state")
		tried++
	}
	if tried != 4 {
		t.Fatalf("tried %d of 4 opens", tried)
	}
	refusedAt(t, "RenameNoReplace(state/HEAD, moved)", confine.RenameNoReplace(root, name, "moved"), "state")
	refusedAt(t, "RenameNoReplace(beside.txt, state/moved)", confine.RenameNoReplace(root, "beside.txt", filepath.Join("state", "moved")), "state")
	unchanged(t, dir)
	for _, gone := range []string{"moved", filepath.Join("real", "moved")} {
		if _, err := os.Lstat(filepath.Join(dir, gone)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s is there: %v", gone, err)
		}
	}
	f, err := confine.Open(root, filepath.Join("real", "HEAD"), os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Open(real/HEAD) = %v", err)
	}
	_ = f.Close()
}

// A junction at the last component of either name is refused by RenameNoReplace, matching
// ErrOutside and naming it, and nothing moves: the junction itself is not moved, and no name is
// moved onto it.
//
// What it costs when this fails: a rename of a junction a repository committed moves a reparse
// point the walk never follows to a name a program trusts, or a move onto one is taken as a move
// into the folder it leads to.
func TestOnWindowsAJunctionAtTheLastComponentIsRefused(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	plantJunction(t, filepath.Join(dir, "real"), filepath.Join(dir, "state"))
	refusedAt(t, "RenameNoReplace(state, moved)", confine.RenameNoReplace(root, "state", "moved"), "state")
	refusedAt(t, "RenameNoReplace(beside.txt, state)", confine.RenameNoReplace(root, "beside.txt", "state"), "state")
	unchanged(t, dir)
	if info, err := os.Lstat(filepath.Join(dir, "state")); err != nil || info.Mode()&fs.ModeIrregular == 0 {
		t.Errorf("state is %v, %v, want the junction as it was", info, err)
	}
	for _, gone := range []string{"moved", filepath.Join("real", "beside.txt")} {
		if _, err := os.Lstat(filepath.Join(dir, gone)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s is there: %v", gone, err)
		}
	}
}

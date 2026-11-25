package confine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// Mkdir with a perm that gives this account read, write and search, 0o755, 0o750, 0o711, 0o777 and
// 0o700, makes each folder this account's with the owner-only list, protected from the folder
// above, narrower than perm's group and other bits. A perm without owner read, write and search,
// 0o500, 0o600 and 0o300, or with a bit beyond the nine, 0o4755 as written and 0o700 with
// fs.ModeSetuid, fs.ModeSetgid or fs.ModeSticky, is refused with ErrNoMode, matching
// errors.ErrUnsupported, before anything is made.
//
// What it costs when this fails: a program that makes its folders 0o755 on every system cannot
// make them on Windows; or one asks for 0o500 so it cannot write there, or for setgid, and believes
// it has that, while the folder is the owner's to write.
func TestMkdirMakesTheOwnerOnlyListAndRefusesAPermItWouldGiveMoreThan(t *testing.T) {
	t.Parallel()
	for _, perm := range []fs.FileMode{0o500, 0o600, 0o300, 0o4755, fs.ModeSetuid | 0o700, fs.ModeSetgid | 0o700, fs.ModeSticky | 0o700} {
		dir := t.TempDir()
		root := rootAt(t, dir)
		r, err := Mkdir(root, filepath.Join("a", "b"), perm)
		if r != nil {
			_ = r.Close()
		}
		if !errors.Is(err, ErrNoMode) || !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("Mkdir with %#o: %v, and want ErrNoMode matching errors.ErrUnsupported", uint32(perm), err)
		}
		if _, err := os.Lstat(filepath.Join(dir, "a")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Mkdir with %#o, refused, made a folder: %v", uint32(perm), err)
		}
	}
	for _, perm := range []fs.FileMode{0o755, 0o750, 0o711, 0o777, 0o700} {
		dir := t.TempDir()
		root := rootAt(t, dir)
		r, err := Mkdir(root, filepath.Join("a", "b"), perm)
		if err != nil {
			t.Fatalf("Mkdir with %#o: %v", uint32(perm), err)
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		ownerOnlyFolder(t, filepath.Join(dir, "a"))
		ownerOnlyFolder(t, filepath.Join(dir, "a", "b"))
	}
}

// On a volume that keeps no access lists, Mkdir is refused with ErrNoAccessList, which matches
// errors.ErrUnsupported, at the first folder it would make, and makes nothing; a folder there
// already is walked into, as nothing is made.
//
// What it costs when this fails: a folder made on a FAT or exFAT volume is every account's, while
// the program believes it private; or a folder that is there can no longer be
// opened through Mkdir there.
func TestMkdirOnAVolumeWithNoAccessListsIsRefusedAndMakesNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "there"), 0o700); err != nil {
		t.Fatal(err)
	}
	root := rootAt(t, dir)
	r, err := mkdir(root, filepath.Join("there", "new", "deeper"), 0o700, func(string) {}, true)
	if r != nil {
		_ = r.Close()
	}
	if !errors.Is(err, ErrNoAccessList) || !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Mkdir on a volume with no access lists: %v, and want ErrNoAccessList matching errors.ErrUnsupported", err)
	}
	if left, err := os.ReadDir(filepath.Join(dir, "there")); err != nil || len(left) != 0 {
		t.Errorf("a refused Mkdir left %v (%v) in the folder there, and want nothing", left, err)
	}
	r, err = mkdir(root, "there", 0o700, func(string) {}, true)
	if err != nil {
		t.Fatalf("Mkdir of a folder there, on a volume with no access lists: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

// Mkdir of a folder whose name ends in a dot, a., or a space, d , is refused, matching
// fs.ErrInvalid, and nothing is made: the make would make the name as written, which every later
// call, os.Root's among them, reads with the end stripped.
//
// What it costs when this fails: Mkdir makes a folder no later call reaches, then fails to open
// the folder it looks for, and leaves a folder nothing can open by its name.
func TestMkdirOfANameEndingInADotOrASpaceIsRefusedAndMakesNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := rootAt(t, dir)
	for _, name := range []string{"a.", "d "} {
		r, err := Mkdir(root, name, 0o700)
		if r != nil {
			_ = r.Close()
		}
		if !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Mkdir of %q: %v, and want a refusal matching fs.ErrInvalid", name, err)
		}
		if left, err := os.ReadDir(dir); err != nil || len(left) != 0 {
			t.Errorf("a refused Mkdir of %q left %v (%v), and want nothing", name, left, err)
		}
	}
}

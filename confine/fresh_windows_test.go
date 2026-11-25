package confine

import (
	"errors"
	"path/filepath"
	"testing"
)

// MkdirFresh's folder holds the owner-only list, as MkdirPrivate's does, and a volume that keeps no
// access lists is refused with ErrNoAccessList, and nothing is made.
//
// What it costs when this fails: a caller's scratch folder, meant for its own secrets, inherits
// the root's grants to other accounts, or is made on a FAT volume where every account reaches it.
func TestMkdirFreshMakesTheOwnerOnlyListAndRefusesAListlessVolume(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := rootAt(t, dir)
	got, name, err := MkdirFresh(root, "job-")
	if err != nil {
		t.Fatal(err)
	}
	got.Close()
	ownerOnlyFolder(t, filepath.Join(dir, name))
	random, _ := inTurn("1111111111111111")
	got, _, err = mkdirFresh(root, "fat-", random, noStep, true)
	if err == nil {
		got.Close()
	}
	if !errors.Is(err, ErrNoAccessList) {
		t.Errorf("MkdirFresh on a volume with no access lists: %v, and want ErrNoAccessList", err)
	}
	if names := namesIn(dir); len(names) != 1 || names[0] != name {
		t.Errorf("the root holds %q, and want only %q", names, name)
	}
}

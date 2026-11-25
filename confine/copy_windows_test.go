package confine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// Each folder CopyTree makes on Windows is this account's, with the protected owner-only list
// Mkdir makes a folder with, the empty one and one inside another too, whatever list the source's
// folder has.
//
// What it costs when this fails: a copied tree's folders inherit the list of the folder they are
// copied into, so another account may write in the copy.
func TestCopyTreeMakesEachFolderWithTheOwnerOnlyList(t *testing.T) {
	t.Parallel()
	from := t.TempDir()
	for _, dir := range []string{filepath.Join("sub", "deep"), "none"} {
		if err := os.MkdirAll(filepath.Join(from, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(from, "sub", "deep", "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	to := t.TempDir()
	if err := CopyTree(rootAt(t, to), rootAt(t, from)); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"sub", filepath.Join("sub", "deep"), "none"} {
		ownerOnlyFolder(t, filepath.Join(to, dir))
	}
}

// A reparse point in src that is neither a link nor a junction, WSL's link, is refused as Below
// refuses one, matching ErrOutside, never fs.ErrInvalid, and naming it, and nothing is made at its
// name in the copy. Where the system will not set a WSL link, it says so and shows nothing,
// as TestOnWindowsAnUnreadReparsePointIsNeverNotExist does: a skip here would need a skip list
// entry, and the row is not in api/contract.txt.
//
// What it costs when this fails: CopyTree's doc names a sentinel the refusal does not match, or a
// reparse point the walk cannot read is copied as a folder of its own.
func TestOnWindowsCopyTreeRefusesAnotherReparsePointAsOutside(t *testing.T) {
	t.Parallel()
	from := t.TempDir()
	if err := PlantOtherReparse(filepath.Join(from, "r")); err != nil {
		t.Logf("the system refuses a WSL link at %s, so there is no reparse point of another tag to copy, and nothing here is shown: %v", from, err)
		return
	}
	to := t.TempDir()
	err := CopyTree(rootAt(t, to), rootAt(t, from))
	if !errors.Is(err, ErrOutside) || errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("CopyTree = %v, and want it to match ErrOutside and not fs.ErrInvalid", err)
	}
	if pe, ok := errors.AsType[*fs.PathError](err); !ok || pe.Path != "r" {
		t.Errorf("CopyTree = %v, and want an *fs.PathError naming r", err)
	}
	if _, err := os.Lstat(filepath.Join(to, "r")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("r is in the copy (%v), and want nothing made", err)
	}
}

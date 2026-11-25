package treesum

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// swapAt plants a tree holding the folders d and other and the files f and g, and gives Sum's
// walk of it with swap called at step for the entry name.
func swapAt(t *testing.T, step, name string, swap func(dir string) error) error {
	t.Helper()
	dir := t.TempDir()
	for _, d := range []string{"d", "other"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"f", "g", filepath.Join("other", "x")} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	done := false
	_, _, err = sum(root, ".", Caps{Entries: 100, Bytes: 100}, func(s, at string) {
		if s == step && at == name {
			done = true
			if err := swap(dir); err != nil {
				t.Fatal(err)
			}
		}
	})
	if !done {
		t.Fatalf("the walk never reached %q", step)
	}
	return err
}

// What it costs when this fails: a file swapped for a link between the listing and the open is
// read through the link, and the digest pins what the link names as the file.
func TestAFileSwappedForALinkBetweenTheListingAndTheOpenIsRefused(t *testing.T) {
	t.Parallel()
	err := swapAt(t, stepFile, "f", func(dir string) error {
		f := filepath.Join(dir, "f")
		return errors.Join(os.Remove(f), os.Symlink("g", f))
	})
	if !errors.Is(err, confine.ErrOutside) {
		t.Fatalf("Sum gave %v, and want the link refused", err)
	}
}

// What it costs when this fails: a folder swapped for a link between its look and its open is
// walked where the link leads, and the digest pins that folder under the name.
func TestAFolderSwappedForALinkBetweenItsLookAndItsOpenIsRefused(t *testing.T) {
	t.Parallel()
	err := swapAt(t, stepFolder, "d", func(dir string) error {
		d := filepath.Join(dir, "d")
		return errors.Join(os.Remove(d), os.Symlink("other", d))
	})
	if !errors.Is(err, confine.ErrOutside) {
		t.Fatalf("Sum gave %v, and want the swapped folder refused", err)
	}
}

package write

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestHeldByRefusesANoIndexVolume: a write that keeps the old file's mode or owner opens that file and
// checks by os.SameFile it is the one looked at; on a Windows volume that gives the file no 64-bit index
// (a WebDAV or SMB drive, or ReFS) heldBy refuses with ErrNoFileIndex rather than trust the compare,
// as every other compare by file ID in write does. Removing the told call in heldBy turns this
// row red.
//
// What it costs when this fails: a kept-mode or kept-owner write on such a drive takes a file swapped in
// at the name for the old one, and keeps that file's mode or owner.
func TestHeldByRefusesANoIndexVolume(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app")
	put0(t, path, "old")
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	var o options
	noFileIndex()(&o)
	KeepMode()(&o)
	err = heldBy(root, "app", fi, o)
	if !errors.Is(err, ErrNoFileIndex) || !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("heldBy on a volume that gives a file no index: %v, want ErrNoFileIndex (matching ErrUnsupported)", err)
	}
}

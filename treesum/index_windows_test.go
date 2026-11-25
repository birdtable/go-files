package treesum

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/internal/fileid"
)

// TestASubtreeOnANoIndexVolumeIsRefused: entering a folder on a Windows volume whose files read a zero
// or all-ones index, where os.SameFile cannot tell the folder from another swapped in between its look
// and its open, is refused with fileid.ErrNoFileIndex, one value with write.ErrNoFileIndex, so
// errors.Is holds with it. The seam makes a normal volume read as one with no index; removing the
// indexAt call by hand turns this row red.
//
// What it costs when this fails: on such a drive a folder swapped in during the walk is summed as the
// one looked at, so the tree's digest covers files the walk never meant.
func TestASubtreeOnANoIndexVolumeIsRefused(t *testing.T) { //nolint:paralleltest // serial: SeamNoIndex moves shared state in internal/fileid
	// serial: SeamNoIndex moves shared state in internal/fileid
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	look, err := root.Stat("sub")
	if err != nil {
		t.Fatal(err)
	}
	defer fileid.SeamNoIndex()()
	sub, err := enter(root, "sub", "sub", look)
	if sub != nil {
		_ = sub.Close()
	}
	if !errors.Is(err, fileid.ErrNoFileIndex) || !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("enter of a folder on a volume that gives it no index: %v, want fileid.ErrNoFileIndex (matching ErrUnsupported)", err)
	}
}

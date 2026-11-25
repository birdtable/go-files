package write

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLinkedLeavesANoIndexFileAtTheName: under NoReplace, linked finds another file at the name it
// just linked, looks at the name twice and takes the file away while the two looks compare as one.
// Neither look is of a file that was told, and on a Windows volume that gives a file no 64-bit index
// (a WebDAV or SMB drive, or some ReFS files) any two files compare as one: so linked reads the
// index of the file at the name first, and leaves a file with none, the swap's refusal joined to
// ErrNoFileIndex. Removing the toldAt call in linked turns this row red: the file is taken away.
//
// What it costs when this fails: on such a drive, a write refused as swapped removes a file another
// process made at the name between linked's two looks.
func TestLinkedLeavesANoIndexFileAtTheName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	put0(t, filepath.Join(dir, "written"), "mine")
	put0(t, filepath.Join(dir, "app"), "theirs")
	mine, err := os.Lstat(filepath.Join(dir, "written"))
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
	err = linked(root, ".app.tmp", "app", mine, o)
	if !errors.Is(err, ErrNoFileIndex) || !errors.Is(err, errors.ErrUnsupported) || !strings.Contains(err.Error(), "was swapped for another file while it was written") {
		t.Errorf("linked over another file with no index: %v, and want the swap's refusal joined to ErrNoFileIndex (matching ErrUnsupported)", err)
	}
	holds(t, filepath.Join(dir, "app"), "theirs")
}

package confine

import (
	"io/fs"
	"testing"
)

// A Windows junction or mount point, which Go reports as irregular and
// os.Root follows, is refused as a link is; a folder, a file and a pipe
// are not links.
//
// What it costs when this fails: a junction on Windows leads out of the root, and
// a write through it lands where os.Root alone would have followed it.
func TestAJunctionIsRefusedAsALinkIs(t *testing.T) {
	t.Parallel()
	ran := 0
	for m, want := range map[fs.FileMode]bool{
		fs.ModeSymlink:     true,
		fs.ModeIrregular:   true,
		fs.ModeDir:         false,
		0:                  false,
		fs.ModeNamedPipe:   false,
		fs.ModeSymlink | 1: true,
	} {
		if got := linkKind(m) != ""; got != want {
			t.Errorf("linkKind(%v) refuses: %v, want %v", m, got, want)
		}
		ran++
	}
	if ran == 0 {
		t.Fatal("no kind was checked")
	}
}

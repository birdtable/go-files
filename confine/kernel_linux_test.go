package confine

import (
	"os"
	"path/filepath"
	"testing"
)

// The kernel itself refuses a name that climbs out of the folder, asked past the check that
// refuses ".." first, so the folder holds even where that check would miss; the sibling it would
// reach is there. It is Linux's: macOS's O_NOFOLLOW_ANY follows no link but climbs, and there the
// check alone refuses "..".
//
// What it costs when this fails: a name that slips past the check opens a file beside the folder,
// since only the kernel then stands in the way.
func TestTheKernelRefusesANameThatClimbsOutOfTheFolder(t *testing.T) {
	t.Parallel()
	outer := t.TempDir()
	must(t, os.Mkdir(filepath.Join(outer, "in"), 0o755))
	must(t, os.WriteFile(filepath.Join(outer, "beside.txt"), []byte("b\n"), 0o644))
	root, err := os.OpenRoot(filepath.Join(outer, "in"))
	must(t, err)
	defer root.Close()
	f, handled, err := sysOpen(kernelOpen).open(root, filepath.Join("..", "beside.txt"), os.O_RDONLY, 0, func(string) {})
	if !handled {
		t.Fatal("the kernel has no openat2")
	}
	refused(t, "../beside.txt asked of the kernel", f, err)
}

//go:build unix

package confine

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// MkdirShared sets 0755 exactly on each folder it makes and MkdirPrivate 0700, whatever the umask,
// and a folder that was there keeps its mode.
//
// What it costs when this fails: a umask of 077 makes a service's shared folder unreachable to the
// accounts it is for, or a umask of 0 makes a private folder open to every account, or a folder
// above is widened or narrowed.
func TestMkdirSharedAndPrivateSetTheirModesUnderAnyUmask(t *testing.T) { //nolint:paralleltest // serial: sets the process umask, which every folder a parallel test made would take
	for _, mask := range []int{0o077, 0} {
		old := syscall.Umask(mask)
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "a"), 0o711); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(dir, "a"), 0o711); err != nil {
			t.Fatal(err)
		}
		root := rootAt(t, dir)
		shared, err := MkdirShared(root, filepath.Join("a", "s", "t"))
		if err == nil {
			shared.Close()
		}
		private, perr := MkdirPrivate(root, filepath.Join("a", "p", "q"))
		if perr == nil {
			private.Close()
		}
		syscall.Umask(old)
		if err != nil || perr != nil {
			t.Fatalf("umask %03o: MkdirShared %v, MkdirPrivate %v", mask, err, perr)
		}
		for d, want := range map[string]fs.FileMode{
			"a":                          0o711,
			filepath.Join("a", "s"):      0o755,
			filepath.Join("a", "s", "t"): 0o755,
			filepath.Join("a", "p"):      0o700,
			filepath.Join("a", "p", "q"): 0o700,
		} {
			if got := modeAt(t, filepath.Join(dir, d)); got != want|fs.ModeDir {
				t.Errorf("umask %03o: %s is %v, and want %v", mask, d, got, want|fs.ModeDir)
			}
		}
	}
}

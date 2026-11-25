//go:build unix

package confine

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// MkdirFresh's folder is 0700 exactly, empty and this account's, under a umask of 077 or 0, and the
// root keeps its mode.
//
// What it costs when this fails: a umask of 0 makes a caller's scratch folder, meant for its own
// secrets, readable by every account.
func TestMkdirFreshMakesItsFolder0700UnderAnyUmask(t *testing.T) { //nolint:paralleltest // serial: sets the process umask, which every folder a parallel test made would take
	for _, mask := range []int{0o077, 0} {
		old := syscall.Umask(mask)
		dir := t.TempDir()
		if err := os.Chmod(dir, 0o711); err != nil {
			t.Fatal(err)
		}
		got, name, err := MkdirFresh(rootAt(t, dir), "job-")
		syscall.Umask(old)
		if err != nil {
			t.Fatalf("umask %o: %v", mask, err)
		}
		got.Close()
		fi, err := os.Lstat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !fi.IsDir() || fi.Mode().Perm() != 0o700 || fi.Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0 {
			t.Errorf("umask %o: the folder is %v, and want a folder of 0700 exactly", mask, fi.Mode())
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Geteuid() {
			t.Errorf("umask %o: the folder is not this account's", mask)
		}
		if names := namesIn(filepath.Join(dir, name)); len(names) != 0 {
			t.Errorf("umask %o: the folder holds %q, and want it empty", mask, names)
		}
		if root, err := os.Stat(dir); err != nil || root.Mode().Perm() != 0o711 {
			t.Errorf("umask %o: the root is %v (%v), and want it left at 0711", mask, root, err)
		}
	}
}

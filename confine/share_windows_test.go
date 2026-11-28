package confine

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// TestARootOnAShareOfThisMachinesOwnDiskIsNotRefused opens a root on the temp folder reached by
// this machine's administrative share, \\localhost\C$\..., and shows that the refusal of a root on
// the pipes' device leaves it alone: the root's own handle is FILE_TYPE_DISK, so the file type, which
// decides where the system reads no final path, passes it, and the final path read from it is not on
// the pipes' device; Open, ReadCapped, IsRunnable and Names of a file in it each answer. UNC paths
// are not refused by go-files, and a root on one is a root on a disk. Where the share cannot be
// reached the test fails, as it then shows nothing.
//
// What it costs when this fails: a program that keeps its files on a share, a mapped home folder
// among them, finds every call of confine refused as if its root were a pipe.
func TestARootOnAShareOfThisMachinesOwnDiskIsNotRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	vol := filepath.VolumeName(dir)
	if len(vol) != 2 {
		t.Fatalf("the control: the temp folder %s is on no drive letter, so no administrative share reaches it and this test shows nothing", dir)
	}
	share := `\\localhost\` + vol[:1] + `$` + dir[2:]
	root, err := os.OpenRoot(share)
	if err != nil {
		t.Fatalf("the control: os.OpenRoot of %s: %v, so this test shows nothing", share, err)
	}
	t.Cleanup(func() { _ = root.Close() })
	f, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	h := windows.Handle(f.Fd())
	if ft, err := windows.GetFileType(h); err != nil || ft != windows.FILE_TYPE_DISK {
		t.Errorf("the file type of the root %s: %d, %v, and want FILE_TYPE_DISK (%d)", share, ft, err, windows.FILE_TYPE_DISK)
	}
	var buf [windows.MAX_LONG_PATH]uint16
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), volumeNameNT)
	t.Logf("native: the final path of the root %s: %q, %v", share, windows.UTF16ToString(buf[:min(int(n), len(buf))]), err)
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rootOnPipes(root, "file"); err != nil {
		t.Errorf("rootOnPipes of the root %s: %v, and want it passed", share, err)
	}
	for _, s := range []struct {
		name string
		call func() error
	}{
		{"Open", func() error {
			f, err := Open(root, "file", os.O_RDONLY, 0)
			if f != nil {
				err = errors.Join(err, f.Close())
			}
			return err
		}},
		{"ReadCapped", func() error { _, err := ReadCapped(root, "file", 1); return err }},
		{"IsRunnable", func() error { _, err := IsRunnable(root, "file"); return err }},
		{"Names", func() error { _, err := Names(root, "file"); return err }},
	} {
		if err := s.call(); err != nil {
			t.Errorf("%s of a file in the root %s: %v, and want it answered", s.name, share, err)
		}
	}
}

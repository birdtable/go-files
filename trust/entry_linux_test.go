package trust

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/sys/unix"
)

const (
	mountRootTest = "TestOnLinuxAMountsRootIsToldByItsMount"
	mountRootArg  = "trust-mount-root-namespace"
)

// On Linux a mount's root is told by its mount, not its device: /proc is one under /, and a
// folder of the test's own is none, nor is a link to /proc. In the user and mount namespace the
// nosuid rows run in, a folder bound over itself, which keeps its disk's device, is a mount's
// root, and its unbound twin beside it is none.
//
// What it costs when this fails: a bind mount, which keeps its disk's device, is not told from the
// folder it covers, so an entry that is no mount's root lends the disk beneath it a pass, or one
// that is a mount's root is refused.
func TestOnLinuxAMountsRootIsToldByItsMount(t *testing.T) {
	if slices.Contains(os.Args, mountRootArg) {
		boundRoot(t)
		return
	}
	t.Parallel()
	d := t.TempDir()
	sub, l := filepath.Join(d, "sub"), filepath.Join(d, "l")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/proc", l); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		entry, parent string
		want          bool
	}{{"/proc", "/", true}, {sub, d, false}, {l, d, false}} {
		if got, err := mountRoot(c.entry, c.parent); err != nil || got != c.want {
			t.Errorf("mountRoot(%s, %s): %v, %v, and want %v", c.entry, c.parent, got, err, c.want)
		}
	}
	rerunUnshared(t, mountRootTest, mountRootArg, "the mount root rows ran")
}

// boundRoot is the rows in the namespace: a folder bound over itself, on the device of the folder
// above it, is a mount's root, and its twin, made beside it and not bound, is none.
func boundRoot(t *testing.T) {
	t.Helper()
	d := t.TempDir()
	bound, twin := filepath.Join(d, "bound"), filepath.Join(d, "twin")
	for _, f := range []string{bound, twin} {
		if err := os.Mkdir(f, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Mount(bound, bound, "", unix.MS_BIND, ""); err != nil {
		t.Fatalf("bind %s over itself: %v", bound, err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(bound, unix.MNT_DETACH); err != nil {
			t.Errorf("unmount %s: %v", bound, err)
		}
	})
	var b, p unix.Stat_t
	if err := unix.Stat(bound, &b); err != nil {
		t.Fatal(err)
	}
	if err := unix.Stat(d, &p); err != nil {
		t.Fatal(err)
	}
	if b.Dev != p.Dev {
		t.Fatalf("%s, bound over itself, is on device %d and %s on %d, and want one device, which no device compare tells apart", bound, b.Dev, d, p.Dev)
	}
	for _, c := range []struct {
		entry string
		want  bool
	}{{bound, true}, {twin, false}} {
		if got, err := mountRoot(c.entry, d); err != nil || got != c.want {
			t.Errorf("mountRoot(%s, %s): %v, %v, and want %v", c.entry, d, got, err, c.want)
		}
	}
	t.Logf("the mount root rows ran on %s", d)
}

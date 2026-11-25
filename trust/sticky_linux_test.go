package trust

import (
	"errors"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test starts unshare, with this test binary again in it; the module has no runner
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// stickyTest is the real-disk row of a folder right in root's sticky folder on another device
// than it, run again in the user and mount namespace unshare makes for it.
const stickyTest = "TestOnARealDiskAFolderMountedRightInRootsStickyFolderIsRefusedAndOneBesideItPasses"

// stickyArg marks this test binary run again for stickyTest, before the folder it mounts on.
const stickyArg = "trust-sticky-device"

// What it costs when this fails: a folder another account serves through a mount right in /tmp,
// on a real disk and not only in the table of a test's own host, passes as the walking account's own.
func TestOnARealDiskAFolderMountedRightInRootsStickyFolderIsRefusedAndOneBesideItPasses(t *testing.T) {
	if i := slices.Index(os.Args, stickyArg); i > 0 && i+1 < len(os.Args) {
		inStickyNamespace(t, os.Args[i+1])
		return
	}
	t.Parallel()
	// No skip: a runner that cannot unshare fails this row, as it fails the overflow rows, since a
	// skip needs an allow-list entry and none grows.
	flags := []string{"--user", "--map-root-user", "--mount"}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := append(flags, exe, "-test.run=^"+stickyTest+"$", "-test.count=1", "-test.v", stickyArg, ownFolder(t))
	out, err := exec.CommandContext(t.Context(), "unshare", args...).CombinedOutput() //nolint:gosec // G204: unshare and this test binary, with literal flags and the test's own folder
	if err != nil || !strings.Contains(string(out), "the sticky device rows ran") {
		t.Errorf("this test in unshare %s: %v:\n%s", strings.Join(flags, " "), err, out)
	}
	t.Logf("%s", out)
}

// inStickyNamespace mounts, on dir, a tmpfs of its own to be the root, with /proc bound in, a
// 1777 tmpfs at /tmp in it, and a third tmpfs on the folder /tmp/f; it changes its root to dir,
// and holds that /tmp/f is refused, on another device than /tmp, and /tmp/g beside it passes.
// Root here is this account, mapped, and owns all three.
func inStickyNamespace(t *testing.T, dir string) {
	t.Helper()
	tmp := filepath.Join(dir, "tmp")
	steps := []struct {
		name string
		do   func() error
	}{
		{"the root tmpfs", func() error { return syscall.Mount("tmpfs", dir, "tmpfs", 0, "mode=0755") }},
		{"/proc", func() error { return os.Mkdir(filepath.Join(dir, "proc"), 0o755) }},
		{"/proc bound", func() error {
			return syscall.Mount("/proc", filepath.Join(dir, "proc"), "", syscall.MS_BIND|syscall.MS_REC, "")
		}},
		{"/tmp", func() error { return os.Mkdir(tmp, 0o755) }},
		{"the sticky tmpfs", func() error { return syscall.Mount("tmpfs", tmp, "tmpfs", 0, "mode=1777") }},
		{"/tmp/f", func() error { return os.Mkdir(filepath.Join(tmp, "f"), 0o700) }},
		{"/tmp/g", func() error { return os.Mkdir(filepath.Join(tmp, "g"), 0o700) }},
		{"the tmpfs on /tmp/f", func() error { return syscall.Mount("tmpfs", filepath.Join(tmp, "f"), "tmpfs", 0, "mode=0700") }},
		{"the root", func() error { return syscall.Chroot(dir) }},
		{"the root, entered", func() error { return os.Chdir("/") }},
	}
	for _, s := range steps {
		if err := s.do(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
	}
	_, err := Path("/tmp/f", Folder, Who{})
	if !errors.Is(err, ErrNotOwners) || !strings.Contains(err.Error(), "on another device than it") {
		t.Errorf("/tmp/f, a tmpfs mounted right in the sticky /tmp: %v, and want ErrNotOwners, on another device than it", err)
	}
	if _, err := Path("/tmp/g", Folder, Who{}); err != nil {
		t.Errorf("/tmp/g, a folder right in the sticky /tmp, on its device: %v, and want a pass", err)
	}
	t.Logf("the sticky device rows ran: /tmp/f %v", err)
}

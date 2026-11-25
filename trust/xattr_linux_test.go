package trust

import (
	"errors"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test starts unshare, with this test binary again in it; the module has no runner
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// xattrTest is the test run again in a user and mount namespace, and xattrArg marks that run,
// before the folder the outer run made.
const (
	xattrTest = "TestAListIsReadOnTheFileHeldNotOnAPathMountedOver"
	xattrArg  = "trust-xattr-namespace"
)

// What it costs when this fails: an owner of this mount namespace mounts over /proc/self/fd and
// answers a clean list, or none, for a name on NFS whose list lets others in, and the name passes.
func TestAListIsReadOnTheFileHeldNotOnAPathMountedOver(t *testing.T) {
	if i := slices.Index(os.Args, xattrArg); i > 0 && i+1 < len(os.Args) {
		overFd(t, os.Args[i+1])
		return
	}
	t.Parallel()
	d := t.TempDir()
	for _, name := range []string{"held", "other"} {
		p := filepath.Join(d, name)
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := unix.Setxattr(p, "user.trust", []byte(name), 0); err != nil {
			t.Fatalf("%s: %v, and the row needs a user attribute on the test's folder", p, err)
		}
	}
	if err := unix.Setxattr(d, "user.trust", []byte("folder"), 0); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(d, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, want string
		err        error
	}{
		{"held", "held", nil},
		{"", "folder", nil},
		{"fifo", "", errNotReopened},
	} {
		fd, err := unix.Open(filepath.Join(d, c.name), unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		b, err := xattrOf(fd, "user.trust")
		unix.Close(fd)
		if string(b) != c.want || !errors.Is(err, c.err) || (c.err == nil) != (err == nil) {
			t.Errorf("%q: %q, %v, and want %q, %v", c.name, b, err, c.want, c.err)
		}
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"--user", "--map-root-user", "--mount", exe, "-test.run=^" + xattrTest + "$", "-test.count=1", "-test.v", xattrArg, d}
	out, err := exec.CommandContext(t.Context(), "unshare", args...).CombinedOutput() //nolint:gosec // G204: unshare and this test binary, with literal flags and the test's own folder
	if err != nil || !strings.Contains(string(out), "the mounted-over rows ran") {
		t.Errorf("this test in unshare --user --map-root-user --mount: %v:\n%s", err, out)
	}
}

// overFd is the test's rows in its own user and mount namespace: d/held is held open with O_PATH,
// and a tmpfs mounted over /proc/<pid>/fd answers d/other at its number. The path, as getxattr
// reads it, then answers other's attribute, and xattrOf refuses the file it reopens there.
func overFd(t *testing.T, d string) {
	t.Helper()
	fd, err := unix.Open(filepath.Join(d, "held"), unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	n := strconv.Itoa(fd)
	pid := "/proc/" + strconv.Itoa(os.Getpid())
	link := pid + "/fd/" + n
	if err := unix.Mount("tmpfs", pid+"/fd", "tmpfs", 0, ""); err != nil {
		t.Logf("a tmpfs over %s/fd: %v, so over %s", pid, err, pid)
		if err := unix.Mount("tmpfs", pid, "tmpfs", 0, ""); err != nil {
			t.Fatalf("a tmpfs over %s: %v", pid, err)
		}
		if err := os.Mkdir(pid+"/fd", 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(d, "other"), link); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 16)
	size, err := unix.Getxattr("/proc/self/fd/"+n, "user.trust", b)
	if err != nil || string(b[:max(size, 0)]) != "other" {
		t.Fatalf("by the path mounted over: %q, %v, and want other's, so the mount took", b[:max(size, 0)], err)
	}
	if got, err := xattrOf(fd, "user.trust"); !errors.Is(err, errNotHeldFile) {
		t.Errorf("xattrOf with /proc/self/fd mounted over: %q, %v, and want errNotHeldFile", got, err)
	}
	// The entry at the held number names no file, so the reopen is ENOENT, which nfs4Of must not read
	// as the held file's answer, as it would ENODATA a FUSE server gave there.
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(d, "absent"), link); err != nil {
		t.Fatal(err)
	}
	if got, err := xattrOf(fd, "user.trust"); !errors.Is(err, errNotHeldFile) || errors.Is(err, unix.ENOENT) {
		t.Errorf("xattrOf with /proc/self/fd naming no file: %q, %v, and want errNotHeldFile, matching no errno", got, err)
	}
	t.Logf("the mounted-over rows ran")
}

//go:build linux

package trusttest_test

import (
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test starts this test binary again, as appdirs' namespace rows do; the module has no runner
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/birdtable/go-files/trust/trusttest"
)

// noDescriptorTest is the test run again in a process of its own, where it may take away every
// descriptor without failing the tests beside it; noDescriptorArg marks that run.
const (
	noDescriptorTest = "TestDenyFailsWhenItsOpenFailsForAReasonOtherThanTheDenial"
	noDescriptorArg  = "trusttest-no-descriptor"
)

// Linux finds a descriptor for an open before it looks at the name, so with none left Deny's open
// fails with EMFILE, not with a refusal of this account, as root or not. The run that takes the
// descriptors away is this test again, alone in a child process.
//
// What it costs when this fails: Deny's open failing for a reason other than the denial skips the
// test that called it, as if this account still opened the name, where it should fail it.
func TestDenyFailsWhenItsOpenFailsForAReasonOtherThanTheDenial(t *testing.T) {
	if slices.Contains(os.Args, noDescriptorArg) {
		p, _ := private(t, false)
		var limit syscall.Rlimit
		if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
			t.Fatal(err)
		}
		none := limit
		none.Cur = 0
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &none); err != nil {
			t.Fatal(err)
		}
		r := call(t, func(r *recorder) { trusttest.Deny(r, p) })
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
			t.Fatal(err)
		}
		r.end()
		if r.skipped || !r.failed || !r.says("not a refusal of this account") {
			t.Errorf("Deny whose open finds no descriptor: skipped %v, failed %v, %q, and want a failure naming the open's error", r.skipped, r.failed, r.said)
		}
		t.Logf("the no-descriptor row ran")
		return
	}
	t.Parallel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(t.Context(), exe, //nolint:gosec // G204: this test binary, with literal flags
		"-test.run=^"+noDescriptorTest+"$", "-test.count=1", "-test.v", noDescriptorArg).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "the no-descriptor row ran") {
		t.Errorf("this test in a process of its own: %v:\n%s", err, out)
	}
}

// mountTest is the test run again in the user and mount namespace unshare makes, where it may
// mount over its own folder; mountArg marks that run.
const (
	mountTest = "TestPermFailsAPrivateNameOnADiskTrustRefusesUntilWhoMountsNamesIt"
	mountArg  = "trusttest-mount-namespace"
)

// Linux's statfs flags for a mount nosuid and noexec, ST_NOSUID and ST_NOEXEC, which syscall does
// not name.
const (
	stNosuid = 0x2
	stNoexec = 0x8
)

// Perm calls trust.Held with no Who, so a private name on a disk trust refuses until Who.Mounts
// names it, one mounted nosuid, as udisks mounts an account's stick, or noexec without nosuid, as
// usbmount does, fails the test, saying the mount, where its mode alone is private. The rows run
// with no root, in the namespace unshare --user --map-root-user --mount makes, on a folder of a
// disk bound over itself, then bound nosuid, then noexec and nodev without nosuid. A folder on a
// disk mounted nosuid or noexec fails the test, as the namespace cannot lift either.
//
// What it costs when this fails: Perm passes as private a name on a disk that the program's own
// trust.Path refuses there, so a test says private what the program will not take; or Perm's doc
// says a refusal no test holds.
func TestPermFailsAPrivateNameOnADiskTrustRefusesUntilWhoMountsNamesIt(t *testing.T) {
	if slices.Contains(os.Args, mountArg) {
		boundPerm(t)
		return
	}
	t.Parallel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// No skip: a runner that cannot unshare fails this row, as it fails trust's namespace rows.
	out, err := exec.CommandContext(t.Context(), "unshare", "--user", "--map-root-user", "--mount", exe, //nolint:gosec // G204: unshare and this test binary, with literal flags
		"-test.run=^"+mountTest+"$", "-test.count=1", "-test.v", mountArg).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "the mount rows ran") {
		t.Errorf("this test in unshare --user --map-root-user --mount: %v:\n%s", err, out)
	}
}

// diskFolder is a folder of the test's own on a disk, ext2/3/4, xfs, btrfs, f2fs or ZFS: one in its
// temp folder, or where that is not on a disk, as a tmpfs /tmp is not, one made in the package's
// folder and removed at the test's end.
func diskFolder(t *testing.T) string {
	t.Helper()
	disk := func(d string) bool {
		var sf syscall.Statfs_t
		if err := syscall.Statfs(d, &sf); err != nil {
			t.Fatal(err)
		}
		// Type is int32 on 32-bit Linux, so it is compared as the 32 bits a magic number is.
		return slices.Contains([]uint32{0xEF53, 0x58465342, 0x9123683E, 0xF2F52010, 0x2FC12FC1}, uint32(sf.Type))
	}
	if d := t.TempDir(); disk(d) {
		return d
	}
	d, err := os.MkdirTemp(".", "perm-mount-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(d); err != nil {
			t.Errorf("removing %s: %v", d, err)
		}
	})
	if d, err = filepath.Abs(d); err != nil || !disk(d) {
		t.Fatalf("neither the test's temp folder nor %s, in the package's folder, is on a disk (%v), so no disk can be mounted nosuid here", d, err)
	}
	return d
}

// boundPerm is the rows in the namespace: a private file in a folder of a disk judged by Perm with
// the folder bound over itself, then bound nosuid, then noexec and nodev without nosuid. A flag
// the mount had is kept on each, as the kernel locks it in a namespace it made.
func boundPerm(t *testing.T) {
	t.Helper()
	d := diskFolder(t)
	p := filepath.Join(d, "name")
	if err := os.WriteFile(p, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o600); err != nil { // past the umask
		t.Fatal(err)
	}
	var sf syscall.Statfs_t
	if err := syscall.Statfs(d, &sf); err != nil {
		t.Fatal(err)
	}
	if sf.Flags&(stNosuid|stNoexec) != 0 {
		t.Fatalf("%s is on a disk mounted nosuid or noexec, which this namespace cannot lift", d)
	}
	kept := keptFlags(int64(sf.Flags)) // int32 on 32-bit Linux
	mount := func(row, src string, flags uintptr) {
		t.Helper()
		if err := syscall.Mount(src, d, "", flags, ""); err != nil {
			t.Fatalf("%s: mount on %s: %v", row, d, err)
		}
	}
	judge := func(row, why string) {
		t.Helper()
		r := call(t, func(r *recorder) { trusttest.Perm(r, p, 0o600) })
		r.end()
		if why == "" && r.failed || why != "" && (!r.failed || !r.says("it is not private to this account") || !r.says(why)) {
			t.Errorf("%s: Perm of %s failed %v, %q, and want it failed saying %q, or passed where that is empty", row, p, r.failed, r.said, why)
		}
	}
	mount("bound over itself", d, syscall.MS_BIND)
	t.Cleanup(func() { // before the folder's removal, which a mount on it would refuse
		if err := syscall.Unmount(d, syscall.MNT_DETACH); err != nil {
			t.Errorf("unmount %s: %v", d, err)
		}
	})
	judge("bound over itself", "")
	mount("bound nosuid", "", syscall.MS_REMOUNT|syscall.MS_BIND|syscall.MS_NOSUID|kept)
	judge("bound nosuid, as udisks mounts an account's stick", "mounted nosuid")
	mount("bound noexec and nodev without nosuid", "", syscall.MS_REMOUNT|syscall.MS_BIND|syscall.MS_NOEXEC|syscall.MS_NODEV|kept)
	judge("bound noexec and nodev without nosuid, as usbmount mounts an account's stick", "mounted noexec without nosuid")
	t.Logf("the mount rows ran on %s", d)
}

// keptFlags is the mount flags of the statfs flags f that a remount must keep: read-only, nodev and
// the atime rule, strictatime where neither noatime nor relatime is read.
func keptFlags(f int64) uintptr {
	const stRdonly, stNodev, stNoatime, stNodiratime, stRelatime = 0x1, 0x4, 0x400, 0x800, 0x1000
	var m uintptr
	for st, ms := range map[int64]uintptr{stRdonly: syscall.MS_RDONLY, stNodev: syscall.MS_NODEV,
		stNoatime: syscall.MS_NOATIME, stNodiratime: syscall.MS_NODIRATIME, stRelatime: syscall.MS_RELATIME} {
		if f&st != 0 {
			m |= ms
		}
	}
	if f&(stNoatime|stRelatime) == 0 {
		m |= syscall.MS_STRICTATIME
	}
	return m
}

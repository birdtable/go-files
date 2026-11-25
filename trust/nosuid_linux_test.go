package trust

import (
	"errors"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test starts unshare, with this test binary again in it, as the other namespace rows do; the module has no runner
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

const (
	nosuidTest = "TestOnLinuxADiskBoundNosuidInAUserNamespaceIsRefusedUntilWhoMountsNamesIt"
	nosuidArg  = "trust-nosuid-namespace"
	noexecTest = "TestOnLinuxADiskBoundNoexecWithoutNosuidInAUserNamespaceIsRefusedUntilWhoMountsNamesIt"
	noexecArg  = "trust-noexec-namespace"
)

// The words of each refusal a mount's flag gives, as plainRefused says them, and the ending each
// has, which names no field of Who. whyEntry is the words of another kind of
// refusal: a Who.Mounts entry that is not an absolute, clean path is the caller's own input, refused
// with ErrMountEntry before any mount is judged, so it says nothing of a mount and has no ending.
const (
	whyNosuid = "mounted nosuid"
	whyNoexec = "mounted noexec without nosuid"
	whyEnding = ", and on no mount named as trusted"
	whyEntry  = "which is not an absolute, clean path"
)

// What it costs when this fails: an account's ext4 stick or image, which udisks, fuse2fs run by
// the account, or a fstab row with user mounts nosuid, passes on the owners its volume holds, so a
// file the stick's maker set to root's passes as root's; or a disk named in Who.Mounts, or one
// mounted with setuid as fstab's user,suid does, is refused. The rows run in the
// user and mount namespace unshare makes, with no root: a folder on a disk is bound over itself
// and made nosuid there, which the kernel reports by fstatfs as it does udisks' mount.
func TestOnLinuxADiskBoundNosuidInAUserNamespaceIsRefusedUntilWhoMountsNamesIt(t *testing.T) {
	// The test run again in the namespace takes the folder diskFolder answers, and no path from its
	// arguments.
	if slices.Contains(os.Args, nosuidArg) {
		boundNosuid(t, diskFolder(t))
		return
	}
	t.Parallel()
	rerunUnshared(t, nosuidTest, nosuidArg, "the nosuid rows ran")
}

// What it costs when this fails: an account's ext4 stick, which usbmount mounts noexec and nodev
// without nosuid, passes on the owners its volume holds, so a file the stick's maker set to
// root's passes as root's, though noexec takes away the setuid program that was the reason to
// pass a mount with setuid; or a disk named in Who.Mounts, or one bound again with exec, is
// refused. The rows run as the nosuid rows do, with no root: a folder on a disk is
// bound over itself and made noexec and nodev there, without nosuid.
func TestOnLinuxADiskBoundNoexecWithoutNosuidInAUserNamespaceIsRefusedUntilWhoMountsNamesIt(t *testing.T) {
	if slices.Contains(os.Args, noexecArg) {
		boundNoexec(t, diskFolder(t))
		return
	}
	t.Parallel()
	rerunUnshared(t, noexecTest, noexecArg, "the noexec rows ran")
}

// rerunUnshared runs the test named test again, with arg, in unshare --user --map-root-user
// --mount, and fails it unless it passes and logs the line that begins with ran.
func rerunUnshared(t *testing.T, test, arg, ran string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// No skip: a runner that cannot unshare fails this row, as it fails the overflow rows.
	args := []string{"--user", "--map-root-user", "--mount", exe, "-test.run=^" + test + "$", "-test.count=1", "-test.v", arg}
	out, err := exec.CommandContext(t.Context(), "unshare", args...).CombinedOutput() //nolint:gosec // G204: unshare and this test binary, with literal flags
	i := strings.Index(string(out), ran)
	if err != nil || i < 0 {
		t.Errorf("this test in unshare --user --map-root-user --mount: %v:\n%s", err, out)
		return
	}
	t.Log(strings.SplitN(string(out[i:]), "\n", 2)[0])
}

// diskFolder is a folder on a disk, ext2/3/4, xfs, f2fs, btrfs or ZFS, whose nosuid trust judges,
// and which Who.Mounts may name: every name above it is one only root may change, as mountEntry
// walks it, in this namespace, where the account is root. It is this test's own temp folder, or
// where that is a tmpfs, as /tmp is on Fedora, or under one others may write, as the sticky /tmp
// is where TMPDIR is unset (the hosted runner's), the package's folder. It fails the
// test, saying why of each, where neither is.
func diskFolder(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	h := rootedHost(t)
	var why []string
	for _, d := range []string{ownFolder(t), wd} {
		var sf unix.Statfs_t
		if err := unix.Statfs(d, &sf); err != nil {
			t.Fatal(err)
		}
		if typ := uint32(sf.Type); !byDevice(typ) && diskKinds(typ) == nil {
			why = append(why, d+" is on no disk, ext2/3/4, xfs, f2fs, btrfs or ZFS")
			continue
		}
		if _, err := walk(filepath.Dir(d), Folder, Who{RootOnly: true}, h); err != nil {
			why = append(why, d+" has a name above it that is not one only root may change: "+err.Error())
			continue
		}
		return d
	}
	t.Fatalf("no folder can be made nosuid and named in Who.Mounts: %s", strings.Join(why, "; "))
	return ""
}

// rootedHost is the namespace rows' host. The namespace maps this account as root and shows every
// other, root's / among them, as the overflow account: the walk trusts that account as root, as
// Who.Overflow does where root is not mapped.
func rootedHost(t *testing.T) host {
	t.Helper()
	h := local()
	if h.overflow == "" {
		t.Fatal("the overflow account is not read")
	}
	h.root = append(slices.Clip(h.root), h.overflow)
	return h
}

// bound is d's statfs flags, and judge and mount for d, on rootedHost's host. judge judges d as
// Path does and wants it refused with the words why, or passed where why is "". mount mounts on d
// and wants its flags read as nosuid and noexec after, so the mount took.
func bound(t *testing.T, d string) (flags int64, judge func(row string, who Who, why string), mount func(row, src string, ms uintptr, nosuid, noexec bool)) {
	t.Helper()
	h := rootedHost(t)
	var sf unix.Statfs_t
	if err := unix.Statfs(d, &sf); err != nil {
		t.Fatal(err)
	}
	judge = func(row string, who Who, why string) {
		t.Helper()
		_, err := walk(d, Folder, who, h)
		is, ending := ErrMount, whyEnding
		if why == whyEntry {
			is, ending = ErrMountEntry, ""
		}
		if why != "" && (!errors.Is(err, is) || !strings.Contains(err.Error(), why) || !strings.HasSuffix(err.Error(), ending)) || why == "" && err != nil {
			t.Errorf("%s: %s judged %v, and want it refused as %q, ending %q, or passed where that is empty", row, d, err, why, ending)
		}
	}
	mount = func(row, src string, ms uintptr, nosuid, noexec bool) {
		t.Helper()
		if err := unix.Mount(src, d, "", ms, ""); err != nil {
			t.Fatalf("%s: mount on %s: %v", row, d, err)
		}
		if n, err := look(d); err != nil || n.nosuid != nosuid || n.noexec != noexec {
			t.Fatalf("%s: %s read as nosuid %v and noexec %v, %v, and want %v and %v, so the mount took", row, d, n.nosuid, n.noexec, err, nosuid, noexec)
		}
	}
	return int64(sf.Flags), judge, mount // int32 on 32-bit Linux
}

// flagged is the words of the refusal the statfs flags f give a disk, or "" for none.
func flagged(f int64) string {
	switch {
	case f&unix.ST_NOSUID != 0:
		return whyNosuid
	case f&unix.ST_NOEXEC != 0:
		return whyNoexec
	}
	return ""
}

// unmounted unmounts d at the test's end, before the temp folder's removal, which a mount on it
// would refuse.
func unmounted(t *testing.T, d string) {
	t.Helper()
	t.Cleanup(func() {
		if err := unix.Unmount(d, unix.MNT_DETACH); err != nil {
			t.Errorf("unmount %s: %v", d, err)
		}
	})
}

// boundNosuid is the nosuid rows in the test's own user and mount namespace: d judged before any
// mount, bound over itself, bound nosuid, and where d was not nosuid already, bound again with
// setuid. A flag the mount had is kept on each, as the kernel locks it in a namespace it made, and
// a disk that was noexec is refused as one, once setuid.
func boundNosuid(t *testing.T, d string) {
	t.Helper()
	f, judge, mount := bound(t, d)
	was, exec, kept := f&unix.ST_NOSUID != 0, f&unix.ST_NOEXEC != 0, keptFlags(f)
	judge("before any mount", Who{}, flagged(f))
	mount("bound over itself", d, unix.MS_BIND, was, exec)
	unmounted(t, d)
	judge("bound over itself", Who{}, flagged(f))
	mount("bound nosuid", "", unix.MS_REMOUNT|unix.MS_BIND|unix.MS_NOSUID|kept, true, exec)
	judge("bound nosuid, as udisks, fuse2fs or fstab's user mounts an account's stick", Who{}, whyNosuid)
	judge("the same, Who.Mounts naming it", Who{Mounts: []string{d}}, "")
	judge("the same, Who.Mounts naming /proc, another device", Who{Mounts: []string{"/proc"}}, whyNosuid)
	t.Chdir(filepath.Dir(d)) // this process runs this test alone, so its working folder is the test's
	judge("the same, Who.Mounts naming it relative to the working folder, which no entry may be", Who{Mounts: []string{filepath.Base(d)}}, whyEntry)
	if !was {
		mount("bound with setuid", "", unix.MS_REMOUNT|unix.MS_BIND|kept, false, exec)
		judge("bound again with setuid, as fstab's user,suid mounts it", Who{}, flagged(f&^unix.ST_NOSUID))
	}
	t.Logf("the nosuid rows ran on %s, nosuid before any mount %v, noexec %v", d, was, exec)
}

// boundNoexec is the noexec rows in the test's own user and mount namespace: d judged before any
// mount, bound over itself, bound noexec and nodev without nosuid, as usbmount mounts an account's
// stick, and where d was not noexec already, bound again with exec. A d mounted nosuid fails the
// test, as a namespace cannot lift a flag its mount had.
func boundNoexec(t *testing.T, d string) {
	t.Helper()
	f, judge, mount := bound(t, d)
	if f&unix.ST_NOSUID != 0 {
		t.Fatalf("%s is on a disk mounted nosuid, which this namespace cannot lift, so no mount noexec without nosuid is made there", d)
	}
	was, kept := f&unix.ST_NOEXEC != 0, keptFlags(f)
	judge("before any mount", Who{}, flagged(f))
	mount("bound over itself", d, unix.MS_BIND, false, was)
	unmounted(t, d)
	mount("bound noexec and nodev without nosuid", "", unix.MS_REMOUNT|unix.MS_BIND|unix.MS_NOEXEC|unix.MS_NODEV|kept, false, true)
	judge("bound noexec and nodev without nosuid, as usbmount mounts an account's stick", Who{}, whyNoexec)
	judge("the same, Who.Mounts naming it", Who{Mounts: []string{d}}, "")
	judge("the same, Who.Mounts naming /proc, another device", Who{Mounts: []string{"/proc"}}, whyNoexec)
	if !was {
		mount("bound with exec", "", unix.MS_REMOUNT|unix.MS_BIND|kept, false, false)
		judge("bound again with exec", Who{}, "")
	}
	t.Logf("the noexec rows ran on %s, noexec before any mount %v", d, was)
}

// keptFlags is the mount flags of the statfs flags f that a remount must keep: read-only, nodev,
// noexec and the atime rule, strictatime where neither noatime nor relatime is read.
func keptFlags(f int64) uintptr {
	var m uintptr
	for st, ms := range map[int64]uintptr{unix.ST_RDONLY: unix.MS_RDONLY, unix.ST_NODEV: unix.MS_NODEV, unix.ST_NOEXEC: unix.MS_NOEXEC,
		unix.ST_NOATIME: unix.MS_NOATIME, unix.ST_NODIRATIME: unix.MS_NODIRATIME, unix.ST_RELATIME: unix.MS_RELATIME} {
		if f&st != 0 {
			m |= ms
		}
	}
	if f&(unix.ST_NOATIME|unix.ST_RELATIME) == 0 {
		m |= unix.MS_STRICTATIME
	}
	return m
}

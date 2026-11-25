package trust

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test starts unshare, with this test binary again in it; the module has no runner
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// What it costs when this fails: the namespace read fails on a real kernel, so on a host every
// FUSE name of every account is refused, which no row of ofInitialNS shows.
func TestThisProcessReadsItsUserNamespacesAsTheInitialOneOnAHost(t *testing.T) {
	t.Parallel()
	m, err := os.ReadFile("/proc/self/uid_map")
	if err != nil {
		t.Fatal(err)
	}
	host := slices.Equal(strings.Fields(string(m)), []string{"0", "0", "4294967295"})
	own, mount := userNS()
	if got := own == mount && own.ino == initUserNS; got != host {
		t.Errorf("the user namespaces %v and %v: read as the initial one %v, where uid_map is %q", own, mount, got, m)
	}
}

// What it costs when this fails: a file mounted over /proc is read as the kernel's mountinfo or
// uid_map; or the real one is refused, so every FUSE name on a host is; or a /proc file that does
// not end is read without end.
func TestWhatIsReadUnderProcIsHeldToProcfs(t *testing.T) {
	t.Parallel()
	held := kernelAtLeast(t, 5, 6)
	if _, err := readMountinfo(); held == errors.Is(err, errNotProc) || held && err != nil {
		t.Errorf("/proc/self/mountinfo: %v, and want it read from Linux 5.6, and errNotProc before", err)
	}
	if b, err := readMountinfoUpTo(8); !errors.Is(err, errProcLong) && held || b != nil {
		t.Errorf("/proc/self/mountinfo past a bound of 8: %q, %v, and want errProcLong", b, err)
	}
	if b, err := readProcUpTo("/proc/self/uid_map", 8); !errors.Is(err, errProcLong) || b != nil {
		t.Errorf("/proc/self/uid_map past a bound of 8: %q, %v, and want errProcLong", b, err)
	}
	if _, err := readProc("/proc/sys/kernel/overflowuid"); err != nil {
		t.Errorf("/proc/sys/kernel/overflowuid: %v, and want it read", err)
	}
	f := filepath.Join(t.TempDir(), "mountinfo")
	if err := os.WriteFile(f, []byte("41 22 0:52 / /home/a rw - virtiofs share rw\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := readProc(f); !errors.Is(err, errNotProc) || b != nil {
		t.Errorf("%s, a file not on procfs: %q, %v, and want errNotProc", f, b, err)
	}
}

// What it costs when this fails: a file mounted over /proc/self/ns, or a namespace of another type,
// is taken for this process's user namespace; or the pidfd calls answer other namespaces than
// /proc/self/ns does, as with a wrong ioctl number.
func TestTheNamespacesAreHeldToNsfsAndTheirTypeAndThePidfdCallsAgreeWithProc(t *testing.T) {
	t.Parallel()
	user, mnt := procNS()
	defer unix.Close(user)
	defer unix.Close(mnt)
	if nsOf(user, unix.CLONE_NEWUSER) == (nsFile{}) || nsOf(mnt, unix.CLONE_NEWNS) == (nsFile{}) {
		t.Fatalf("/proc/self/ns/user and mnt are not read as namespaces of their types")
	}
	if nsOf(user, unix.CLONE_NEWNS) != (nsFile{}) {
		t.Errorf("the user namespace is read as a mount namespace")
	}
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if nsOf(int(f.Fd()), unix.CLONE_NEWUSER) != (nsFile{}) {
		t.Errorf("a file of procfs is read as a namespace")
	}
	pu, pm, ok := pidfdNS()
	if !ok && kernelAtLeast(t, 6, 11) {
		t.Fatalf("this kernel, 6.11 or later, answers no pidfd namespace call, as with a wrong ioctl number")
	}
	if !ok {
		t.Logf("this kernel has no pidfd namespace calls, so userNS reads /proc/self/ns")
		return
	}
	defer unix.Close(pu)
	defer unix.Close(pm)
	if nsOf(pu, unix.CLONE_NEWUSER) != nsOf(user, unix.CLONE_NEWUSER) || nsOf(pm, unix.CLONE_NEWNS) != nsOf(mnt, unix.CLONE_NEWNS) {
		t.Errorf("the pidfd calls answer %v and %v, and /proc/self/ns %v and %v", nsOf(pu, unix.CLONE_NEWUSER), nsOf(pm, unix.CLONE_NEWNS),
			nsOf(user, unix.CLONE_NEWUSER), nsOf(mnt, unix.CLONE_NEWNS))
	}
	t.Logf("the pidfd namespace calls ran, and agree with /proc/self/ns")
}

// What it costs when this fails: before Linux 6.11, where a pidfd has no namespace calls, every
// FUSE name judged closes a descriptor it never opened, as x/sys answers 0 with a failed call: the
// program's stdin, or a file another goroutine opened there, which is then reused under it.
// The failed calls here answer a descriptor of the test's own, on any
// kernel.
func TestAFailedPidfdCallClosesNoDescriptorItDidNotOpen(t *testing.T) {
	t.Parallel()
	open := func(t *testing.T) (int, unix.Stat_t) {
		t.Helper()
		fd, err := unix.Open(t.TempDir(), unix.O_PATH|unix.O_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { unix.Close(fd) })
		var st unix.Stat_t
		if err := unix.Fstat(fd, &st); err != nil {
			t.Fatal(err)
		}
		return fd, st
	}
	for _, failed := range []uint{pidfdGetUserNS, pidfdGetMntNS} {
		mine, was := open(t)
		ioctl := func(_ int, req uint) (int, error) {
			if req == failed {
				return mine, unix.ENOTTY
			}
			return unix.Open(t.TempDir(), unix.O_PATH|unix.O_CLOEXEC, 0)
		}
		user, mnt, ok := nsOfPidfd(-1, ioctl)
		var st unix.Stat_t
		if err := unix.Fstat(mine, &st); err != nil || st.Dev != was.Dev || st.Ino != was.Ino {
			t.Errorf("the call %#x failed: the test's descriptor %d, which it answered, is closed (%v)", failed, mine, err)
		}
		if ok || user != -1 || mnt != -1 {
			t.Errorf("the call %#x failed: answered %d and %d, %v, and want -1, -1, false", failed, user, mnt, ok)
		}
	}
}

const (
	procTest = "TestAFileOfProcfsMountedOrLinkedOverTheOneAskedForIsNotRead"
	procArg  = "trust-proc-namespace"
)

// What it costs when this fails: an owner of this mount namespace binds another procfs file, as a
// process's own cmdline, over /proc/self/mountinfo, or mounts a tmpfs over /proc with a link there
// to one, and a FUSE name is judged on the lines it holds and passes;
// or /proc/sys bound over itself, as in every runc container, refuses every call.
func TestAFileOfProcfsMountedOrLinkedOverTheOneAskedForIsNotRead(t *testing.T) {
	if i := slices.Index(os.Args, procArg); i > 0 && i+1 < len(os.Args) {
		overProc(t, os.Args[i+1])
		return
	}
	t.Parallel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"--user", "--map-root-user", "--mount", exe, "-test.run=^" + procTest + "$", "-test.count=1", "-test.v", procArg, t.TempDir()}
	out, err := exec.CommandContext(t.Context(), "unshare", args...).CombinedOutput() //nolint:gosec // G204: unshare and this test binary, with literal flags and the test's own folder
	if err != nil || !strings.Contains(string(out), "the mounted-over rows ran") {
		t.Errorf("this test in unshare --user --map-root-user --mount: %v:\n%s", err, out)
	}
}

// overProc is the test's rows in its own user and mount namespace, each mount over the last: the
// files read before any; /proc/sys bound over itself; this process's cmdline bound over
// mountinfo; a tmpfs over /proc with a link at self/mountinfo to the cmdline of /proc bound at
// d/proc; and on that tmpfs a plain file at self/mountinfo, which only procRoot's hold refuses.
// Before Linux 5.6 every mountinfo read is errNotProc.
func overProc(t *testing.T, d string) {
	t.Helper()
	held := kernelAtLeast(t, 5, 6)
	read := func(row, path string, read func() ([]byte, error), ok bool) {
		t.Helper()
		b, err := read()
		if ok != (err == nil) || !ok && !errors.Is(err, errNotProc) {
			t.Errorf("%s: %s read %.40q, %v, and want it read %v, or errNotProc", row, path, b, err, ok)
		}
	}
	ns := func(row string, ok bool) {
		t.Helper()
		user, mnt := procNS()
		defer unix.Close(user)
		defer unix.Close(mnt)
		if got := nsOf(user, unix.CLONE_NEWUSER) != (nsFile{}) && nsOf(mnt, unix.CLONE_NEWNS) != (nsFile{}); got != ok {
			t.Errorf("%s: /proc/self/ns read as this process's namespaces %v, and want %v", row, got, ok)
		}
	}
	mount := func(src, dst, typ string, flags uintptr) {
		t.Helper()
		if err := unix.Mount(src, dst, typ, flags, ""); err != nil {
			t.Fatalf("mount %s on %s: %v", src, dst, err)
		}
	}
	const uid, info = "/proc/sys/kernel/overflowuid", "/proc/self/mountinfo"
	overflow := func() ([]byte, error) { return readProc(uid) }
	read("before any mount", info, readMountinfo, held)
	read("before any mount", uid, overflow, true)
	ns("before any mount", true)

	mount("/proc/sys", "/proc/sys", "", unix.MS_BIND|unix.MS_REC)
	read("/proc/sys bound over itself", uid, overflow, true)
	read("/proc/sys bound over itself", info, readMountinfo, held)

	mount("/proc/self/cmdline", info, "", unix.MS_BIND)
	if b, err := os.ReadFile(info); err != nil || !strings.Contains(string(b), procArg) {
		t.Fatalf("%s by its path: %.40q, %v, and want this process's cmdline, so the bind took", info, b, err)
	}
	read("cmdline bound over mountinfo", info, readMountinfo, false)

	aside := filepath.Join(d, "proc")
	if err := os.Mkdir(aside, 0o700); err != nil {
		t.Fatal(err)
	}
	mount("/proc", aside, "", unix.MS_BIND|unix.MS_REC)
	mount("tmpfs", "/proc", "tmpfs", 0)
	if err := os.Mkdir("/proc/self", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(aside, strconv.Itoa(os.Getpid()), "cmdline"), info); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(info); err != nil || !strings.Contains(string(b), procArg) {
		t.Fatalf("%s by its link: %.40q, %v, and want this process's cmdline, so the link took", info, b, err)
	}
	read("a tmpfs over /proc, linked to cmdline", info, readMountinfo, false)
	if err := os.Remove(info); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(info, []byte("41 22 0:99 / /m rw - virtiofs x rw\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	read("a tmpfs over /proc, a plain file at mountinfo", info, readMountinfo, false)
	ns("a tmpfs over /proc", false)
	t.Logf("the mounted-over rows ran")
}

// kernelAtLeast is whether uname's release is major.minor or later.
func kernelAtLeast(t *testing.T, major, minor int) bool {
	t.Helper()
	var u unix.Utsname
	if err := unix.Uname(&u); err != nil {
		t.Fatal(err)
	}
	rel := unix.ByteSliceToString(u.Release[:])
	var got [2]int
	for i, s := range strings.SplitN(rel, ".", 3)[:2] {
		n, err := strconv.Atoi(strings.TrimRightFunc(s, func(r rune) bool { return r < '0' || r > '9' }))
		if err != nil {
			t.Fatalf("uname's release %q: %v", rel, err)
		}
		got[i] = n
	}
	return got[0] > major || got[0] == major && got[1] >= minor
}

// What it costs when this fails: fsOf reads a type, or a nosuid or noexec flag, that is not the
// mount's, so every name on Linux is refused, or one on procfs, or on a disk mounted nosuid or
// noexec, passes on the owners it shows.
func TestAMountsTypeAndNosuidAreReadByFstatfsOnTheNameHeld(t *testing.T) {
	t.Parallel()
	if n, err := look("/proc/self/status"); err != nil || n.fsType != unix.PROC_SUPER_MAGIC || !n.typed || n.kind != "proc" {
		t.Errorf("/proc/self/status: of type %#x, typed %v, its mount's %q, %v, and want procfs's, by fstatfs and mountinfo", n.fsType, n.typed, n.kind, err)
	}
	if _, err := Path("/proc/self/status", File, Who{}); !errors.Is(err, ErrMount) || !strings.Contains(err.Error(), "/proc is on a file system of type 0x9fa0") {
		t.Errorf("/proc/self/status: %v, and want ErrMount at /proc", err)
	}
	// ReadOwned judges both halves, and joins the opt-ins of each Who it is given.
	me := "uid:" + strconv.Itoa(os.Geteuid())
	status := "/proc/" + strconv.Itoa(os.Getpid()) + "/status"
	if _, err := ReadOwned(status, me, false, 1<<16); !errors.Is(err, ErrMount) {
		t.Errorf("ReadOwned of %s: %v, and want ErrMount", status, err)
	}
	if _, err := ReadOwned(status, me, false, 1<<16, Who{FileSystems: []uint32{unix.PROC_SUPER_MAGIC}}, Who{Mounts: []string{"/proc"}}); err != nil {
		t.Errorf("ReadOwned of %s, procfs and /proc named: %v, and want it read", status, err)
	}
	d := t.TempDir()
	var sf unix.Statfs_t
	if err := unix.Statfs(d, &sf); err != nil {
		t.Fatal(err)
	}
	n, err := look(d)
	if want := sf.Flags&unix.ST_NOSUID != 0; err != nil || n.fsType != uint32(sf.Type) || n.nosuid != want {
		t.Errorf("%s: of type %#x, nosuid %v, %v, and want %#x, %v", d, n.fsType, n.nosuid, err, uint32(sf.Type), want)
	}
	// procfs is mounted noexec as every distribution mounts it, so its flag is read as set, and a
	// test folder's as statfs reads it.
	var pf unix.Statfs_t
	if err := unix.Statfs("/proc/self/status", &pf); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		path string
		want bool
	}{{"/proc/self/status", pf.Flags&unix.ST_NOEXEC != 0}, {d, sf.Flags&unix.ST_NOEXEC != 0}} {
		if got, err := look(c.path); err != nil || got.noexec != c.want {
			t.Errorf("%s: noexec %v, %v, and want %v, as statfs reads it", c.path, got.noexec, err, c.want)
		}
	}
	// It fails, by intent, and never skips, where /proc is mounted without noexec, as a chroot's
	// hand-made mount may be: a row that cannot read the mark it guards fails, as one that cannot
	// unshare does.
	if pf.Flags&unix.ST_NOEXEC == 0 {
		t.Errorf("/proc is not mounted noexec here, so the row does not show the flag read as set")
	}
	// ext2/3/4, xfs and f2fs are held to a device whose major is not 0, and their mount's type is not
	// read; any other type's is read by its number, from Linux 5.8, and agrees with
	// fstatfs's, a btrfs subvolume's included, on a test folder no 9p server holds.
	if dev := byDevice(n.fsType); dev != (n.kind == "") || n.anon || n.kind == "9p" || !agrees(n.fsType, n.kind) {
		t.Errorf("%s: of type %#x, its mount's %q, major 0 %v, and want a disk's device, or its mount's type read and agreeing", d, n.fsType, n.kind, n.anon)
	}
}

// What it costs when this fails: a ZFS name whose TrueNAS SCALE list lets another account in passes
// on its mode, or one whose list was not read, as when the reopen is not the file held, is read as
// having none.
func TestAZFSNameIsJudgedByWhetherItsHeldFileCarriesAnNFSv4List(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name       string
		err        error
		list, fail bool
	}{
		{"a list kept", nil, true, false},
		{"none, ENODATA", unix.ENODATA, false, false},
		{"none, EOPNOTSUPP", unix.EOPNOTSUPP, false, false},
		{"ENODATA, but not from the file held", fmt.Errorf("%w: %v", errNotHeldFile, unix.ENODATA), false, true}, //nolint:errorlint // as xattrOf builds it
		{"another error", unix.ENOENT, false, true},
	} {
		n, err := zfsOf(node{fsType: zfsMagic}, "/tank/f", c.err)
		if n.zfsList != c.list || (err != nil) != c.fail {
			t.Errorf("%s: list %v, %v, and want list %v, failing %v", c.name, n.zfsList, err, c.list, c.fail)
		}
	}
	// fsOf's ZFS branch, given the type on names of the test's own disk: a socket held, as under a
	// ZFS home, is not reopened, so its list is unread and it is refused unjudged;
	// a file, whose disk keeps no system.nfs4_acl_xdr, carries none.
	d := t.TempDir()
	s, f := filepath.Join(d, "s"), filepath.Join(d, "f")
	if err := socket(t, s); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// Before Linux 5.8 its mount's type is not read, so neither is its list: plainRefused refuses it.
	read := kernelAtLeast(t, 5, 8)
	for _, c := range []struct {
		path string
		want error
	}{{s, errNotReopened}, {f, nil}} {
		fd, err := unix.Open(c.path, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !read {
			c.want = nil
		}
		n, err := typedOf(node{fsType: zfsMagic}, c.path, fd, readMountinfo)
		if !errors.Is(err, c.want) || (c.want == nil) != (err == nil) || n.zfsList {
			t.Errorf("%s, typed ZFS: list %v, %v, and want none, %v", c.path, n.zfsList, err, c.want)
		}
		if err := unix.Close(fd); err != nil {
			t.Error(err)
		}
	}
}

// What it costs when this fails: a 9p share of a host's ext4 has no way through, though the app
// names 9p; or, before Linux 5.8 or where /proc is another pid namespace's, an NFS or ZFS name fails
// on its list read, and any typed name as not there, where its mount's type, not read, refuses it
// with ErrMount.
func TestAMountsTypeIsReadOnMajor0AndAMountNotReadIsReadNoFurther(t *testing.T) {
	t.Parallel()
	if err := notProcOpen(unix.ENOENT); !errors.Is(err, errNotProc) || errors.Is(err, fs.ErrNotExist) {
		t.Errorf("openat2's ENOENT on self/mountinfo: %v, and want errNotProc, matching no name-not-there", err)
	}
	gone := func() ([]byte, error) {
		return nil, &fs.PathError{Op: "openat2", Path: "/proc/self/mountinfo", Err: notProcOpen(unix.ENOENT)}
	}
	unasked := func() ([]byte, error) {
		t.Error("mountinfo read for a disk's type on a disk's device")
		return nil, nil
	}
	s := filepath.Join(t.TempDir(), "s")
	if err := socket(t, s); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(s, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	id, err := mountID(fd)
	nine, ninep := func() ([]byte, error) { return []byte(id + " 1 0:99 / /s rw - 9p host rw\n"), nil }, "9p"
	if err != nil { // before Linux 5.8 no mount is numbered, so none is read
		nine, ninep = gone, ""
	}
	const ext4 = 0xEF53
	for _, c := range []struct {
		name string
		n    node
		read func() ([]byte, error)
		kind string
	}{
		{"ext4 on a device of major 0, its mount 9p's", node{fsType: ext4, id: fileID{vol: unix.Mkdev(0, 99)}}, nine, ninep},
		{"ext4 on a disk's device", node{fsType: ext4, id: fileID{vol: unix.Mkdev(8, 1)}}, unasked, ""},
		{"NFS, /proc another pid namespace's", node{fsType: unix.NFS_SUPER_MAGIC, typ: fs.ModeSocket}, gone, ""},
		{"ZFS, /proc another pid namespace's", node{fsType: zfsMagic, typ: fs.ModeSocket}, gone, ""},
		{"tmpfs, /proc another pid namespace's", node{fsType: unix.TMPFS_MAGIC, typ: fs.ModeSocket}, gone, ""},
		{"FUSE, /proc another pid namespace's", node{fsType: unix.FUSE_SUPER_MAGIC, typ: fs.ModeSocket}, gone, ""},
	} {
		n, err := typedOf(c.n, s, fd, c.read)
		if err != nil || n.kind != c.kind || n.mounter != "" {
			t.Errorf("%s: its mount's %q, mounter %q, %v, and want %q, no list or mounter read, and no error", c.name, n.kind, n.mounter, err, c.kind)
		}
	}
}

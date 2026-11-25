package trusttest

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/birdtable/go-files/trust"
)

// notHere is the words of each skip of a namespace Bind may not mount in.
const notHere = "not in a mount namespace this process may mount in"

// firstNamespace is the words of the skip in the first user namespace, root's on the host among
// it, where a mount would be the system's.
const firstNamespace = "this is the first user namespace, whose mounts are the system's"

// marker is the name of the folder Bind binds: a mount at an entry whose root, as mountinfo gives
// it, ends in a folder so named is a Bind's, whichever process made it.
const marker = "trusttest-bind"

// bind is Bind with entries, the folders it may mount over, in the order it tries them, and h.
func bind(t testing.TB, how Bound, entries []string, h hook) (entry, dir string) {
	t.Helper()
	attr := uint64(unix.MOUNT_ATTR_NOSUID)
	if how == NoexecWithoutNosuid {
		attr = unix.MOUNT_ATTR_NOEXEC | unix.MOUNT_ATTR_NODEV
	}
	if m, err := os.ReadFile("/proc/self/uid_map"); err != nil || slices.Equal(strings.Fields(string(m)), []string{"0", "0", "4294967295"}) {
		t.Skipf("trusttest.Bind: %s: %s, as /proc/self/uid_map, which maps every id to itself, says (%q, %v)", notHere, firstNamespace, m, err)
	}
	if _, err := trust.Path("/", trust.Folder, trust.Who{Overflow: true}); errors.Is(err, trust.ErrOverflow) {
		t.Skipf("trusttest.Bind: %s, one unshare --user --map-current-user makes, mapping this account and no root: %v", notHere, err)
	}
	src := filepath.Join(t.TempDir(), marker)
	if err := os.MkdirAll(filepath.Join(src, "dir"), 0o700); err != nil { // dir before the mount, so each Bind's shows the same
		t.Fatalf("trusttest.Bind: %v", err)
	}
	var sf unix.Statfs_t
	if err := unix.Statfs(src, &sf); err != nil {
		t.Fatalf("trusttest.Bind: statfs %s: %v", src, err)
	}
	if how == NoexecWithoutNosuid && int64(sf.Flags)&unix.ST_NOSUID != 0 { // int32 on 32-bit Linux
		t.Skipf("trusttest.Bind: the temp folder %s is on a disk mounted nosuid, which no namespace lifts, so no mount noexec without nosuid is made of it", src)
	}
	entry = place(t, src, attr, entries, h)
	dir = filepath.Join(entry, "dir")
	_, without := trust.Path(dir, trust.Folder, trust.Who{Overflow: true})
	_, with := trust.Path(dir, trust.Folder, trust.Who{Overflow: true, Mounts: []string{entry}})
	switch {
	case without == nil:
		t.Skipf("trusttest.Bind: the temp folder %s is on a file system whose flags trust does not judge, as a tmpfs /tmp: %s passes with no Who.Mounts", src, dir)
	case !errors.Is(without, trust.ErrMount) || with != nil:
		t.Fatalf("trusttest.Bind: %s, bound over %s, is judged %v with no Who.Mounts and %v with it, and want it refused with ErrMount, then passed", dir, entry, without, with)
	}
	return entry, dir
}

// held is the entries a Bind of this process, its test not yet ended, has mounted over, under its
// lock, which a Bind holds from its choice to its mount: parallel tests share one mount namespace.
var held = struct { //nolint:gochecknoglobals // parallel Binds in one test binary share the mount namespace, so the entries they hold are the process's
	sync.Mutex
	entries map[string]bool
}{entries: map[string]bool{}}

// place binds src over the first of entries that taken passes and whose mount is the first there,
// with attr set and its own flags kept, or skips. At the test's end it unmounts it and lets it go.
func place(t testing.TB, src string, attr uint64, entries []string, h hook) string {
	t.Helper()
	held.Lock()
	defer held.Unlock() // a skip or failure below ends the goroutine, which runs it still
	whys := make([]string, 0, len(entries))
	for _, e := range entries {
		if why := taken(t, e, h); why != "" {
			whys = append(whys, why)
			continue
		}
		fd, id := mountOver(t, src, attr, e)
		if h.between != nil && testing.Testing() {
			held.Unlock() // between may end another test's Bind, whose cleanup takes the lock
			h.between()
			held.Lock()
		}
		if lost(t, fd, id, e) {
			whys = append(whys, e+" was bound by another Bind first, so this one's mount over it was undone")
			continue
		}
		held.entries[e] = true
		t.Cleanup(func() { // before the temp folder's removal, which the mount would refuse
			err := detach(fd, id)
			held.Lock()
			defer held.Unlock()
			if err != nil {
				t.Errorf("trusttest.Bind: unmount %s: %v", e, err)
				return // still mounted, so still held
			}
			delete(held.entries, e)
		})
		return e
	}
	t.Skipf("trusttest.Bind: no folder qualifies as the entry, a folder real root made that no other Bind holds: %s", strings.Join(whys, "; "))
	return ""
}

// taken is why e may not be the entry, or "": a Bind of this process holds it; mountinfo shows a
// Bind's mount at it, another process's in this namespace or one left by a process that ended,
// which holds it while the namespace lasts; or it is no folder real root made, whatever it holds,
// with only names root may change above it, as trust judges it with Who.RootOnly and Who.Overflow.
// h.blind drops all three, as for a Bind whose checks came before another's mount. Its names are
// hidden in the test's own mount namespace alone, until the mount over it ends.
func taken(t testing.TB, e string, h hook) string {
	t.Helper()
	if h.blind && testing.Testing() {
		return ""
	}
	if held.entries[e] {
		return e + " is another test's Bind's until that test ends"
	}
	ms, err := mounts()
	if err != nil {
		t.Fatalf("trusttest.Bind: %v", err)
	}
	for _, m := range ms {
		if m.bound(e) {
			return e + " is held by another process's Bind in this namespace, or by one whose process ended without its unmount, which holds it while the namespace lasts"
		}
	}
	if _, err := trust.Path(e, trust.Folder, trust.Who{RootOnly: true, Overflow: true}); err != nil {
		return err.Error()
	}
	return ""
}

// mountOver binds src over e, a mount with attr set before it is placed and its own flags kept, by
// open_tree, mount_setattr and move_mount, Linux 5.12's, and gives its handle and its id, or skips
// where it may not mount, naming the call refused: open_tree before Linux 5.2, mount_setattr from
// 5.2 to 5.11, and any of the three a seccomp profile older than the kernel refuses. mount_setattr
// comes before statx, which gives no mount id before 5.8, so no kernel before 5.12 reaches statx.
// A mount made at a name stands on the topmost there, so another Bind that reads e
// while this one stands over it finds the same flags and the same dir. A copy never placed ends
// with its handle.
func mountOver(t testing.TB, src string, attr uint64, e string) (fd int, id string) {
	t.Helper()
	fd, err := unix.OpenTree(unix.AT_FDCWD, src, unix.OPEN_TREE_CLONE|unix.OPEN_TREE_CLOEXEC)
	if err != nil {
		t.Skipf("trusttest.Bind: %s: open_tree of %s, which Linux 5.2 has and a seccomp profile older than its kernel may refuse: %s, %v", notHere, src, errnoOf(err), err)
	}
	if err = unix.MountSetattr(fd, "", unix.AT_EMPTY_PATH, &unix.MountAttr{Attr_set: attr}); err != nil {
		t.Skipf("trusttest.Bind: %s: mount_setattr of the copy of %s, which Linux 5.12 has and a seccomp profile older than its kernel may refuse: %s, %v", notHere, src, errnoOf(err), errors.Join(err, unix.Close(fd)))
	}
	var sx unix.Statx_t // the copy's id is its own from open_tree on, and move_mount keeps it
	err = unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &sx)
	if id, err = mountID(sx, err); err != nil {
		t.Fatalf("trusttest.Bind: statx of the copy of %s: %v", src, errors.Join(err, unix.Close(fd)))
	}
	if err = unix.MoveMount(fd, "", unix.AT_FDCWD, e, unix.MOVE_MOUNT_F_EMPTY_PATH); err != nil {
		t.Skipf("trusttest.Bind: %s: move_mount of the copy of %s over %s, which Linux 5.12 has and a seccomp profile older than its kernel may refuse: %s, %v", notHere, src, e, errnoOf(err), errors.Join(err, unix.Close(fd)))
	}
	return fd, id
}

// mountID is the mount id statx gave in sx, err its error, or an error where err is one or sx
// holds no mount id.
func mountID(sx unix.Statx_t, err error) (string, error) {
	if err == nil && sx.Mask&unix.STATX_MNT_ID == 0 {
		err = errors.New("it gives no mount id")
	}
	if err != nil {
		return "", err
	}
	return strconv.FormatUint(sx.Mnt_id, 10), nil
}

// lost is whether the mount numbered id, of handle fd, just placed over e, has lost e to another
// Bind's, by mountinfo read now, and if so detaches it. A mount made at a name stands on the
// topmost there, so the mounts at e stand in the order they were made, whatever their ids, which the
// kernel reuses: one that stands on a Bind's mount at e was made after it. One mountinfo no longer
// shows has lost e too: a Bind beneath that left, losing or at its test's end, took this one's off
// first, as detach does whatever stands on its own. The handle, open still, keeps id from being
// reused, so a mount not found is never another's.
func lost(t testing.TB, fd int, id, e string) bool {
	t.Helper()
	ms, err := mounts()
	if err != nil {
		t.Fatalf("trusttest.Bind: %v", errors.Join(err, detach(fd, id)))
	}
	if own, ok := ms[id]; ok && !ms[own.parent].bound(e) {
		return false
	}
	if err := detach(fd, id); err != nil {
		t.Fatalf("trusttest.Bind: undoing its mount over %s, which another Bind's holds: %v", e, err)
	}
	return true
}

// detach unmounts the mount numbered id, of handle fd, and closes the handle. An unmount through
// the handle, as a walk of any name, reaches the topmost mount standing on the handle's mount's
// root: the handle's own, or a later Bind's mount over it, which loses e and goes on when it finds
// itself off. So detach unmounts until mountinfo no longer shows id, at most once for each mount
// it shows. A mount already off, as one a Bind beneath took off, is no error.
func detach(fd int, id string) error {
	p := "/proc/self/fd/" + strconv.Itoa(fd)
	return detachBy(fd, id, func() error { return unix.Unmount(p, unix.MNT_DETACH) }, mounts)
}

// detachBy is detach, unmounting by unmount and reading the mounts by read: the system's, or in
// trusttest's own tests a mount that stands still.
func detachBy(fd int, id string, unmount func() error, read func() (map[string]mount, error)) error {
	var err error
	for n := 0; ; n++ {
		if err = unmount(); err != nil && !errors.Is(err, unix.EINVAL) {
			break // EINVAL is a handle whose mount is off already, in no namespace
		}
		var ms map[string]mount
		if ms, err = read(); err != nil {
			break
		}
		if _, on := ms[id]; !on {
			break
		}
		if n >= len(ms) {
			err = errors.New("mount " + id + " stands still after an unmount through its handle for each mount in mountinfo")
			break
		}
	}
	return errors.Join(err, unix.Close(fd))
}

// mount is a line of /proc/self/mountinfo: its parent's id, its root and its mount point.
type mount struct{ parent, root, point string }

// bound is whether m is a Bind's mount at e: its mount point is e and its root ends in marker,
// or in marker and "//deleted", as the kernel shows a root whose folder is removed.
func (m mount) bound(e string) bool {
	r := strings.TrimSuffix(m.root, "//deleted")
	return m.point == e && r[strings.LastIndex(r, "/")+1:] == marker
}

// mounts is this process's mounts by id, read from /proc/self/mountinfo by mountsIn.
func mounts() (map[string]mount, error) {
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	return mountsIn(string(b)), nil
}

// mountsIn is the mounts by id mountinfo, the text of /proc/self/mountinfo, holds, each root and
// mount point with its octal escapes read. A line is split on the one byte 0x20 the kernel writes
// between fields and on no other, as trust splits it: the kernel escapes a space, tab, newline or
// backslash within a field, and no other byte, so a temp folder named with a \r or U+00A0 would
// otherwise shift a root and its mount point, and two processes' Binds stack on one entry.
func mountsIn(mountinfo string) map[string]mount {
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	ms := map[string]mount{}
	for line := range strings.Lines(mountinfo) {
		if f := strings.Split(strings.TrimSuffix(line, "\n"), " "); len(f) >= 5 {
			ms[f[0]] = mount{f[1], unescape.Replace(f[3]), unescape.Replace(f[4])}
		}
	}
	return ms
}

// errnoOf is the name of the errno err holds, as ENOSYS or EPERM, so a skip of a mount refused
// says whether the kernel lacks the call or a seccomp profile refused it, or "no errno".
func errnoOf(err error) string {
	if e, ok := errors.AsType[unix.Errno](err); ok && unix.ErrnoName(e) != "" {
		return unix.ErrnoName(e)
	}
	return "no errno"
}

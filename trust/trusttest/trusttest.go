// Package trusttest changes who may read or write a path, and checks the mode a path keeps, for
// the tests of a program that judges its paths with trust: one intent each, which is mode bits on
// Unix and an access list on Windows. A change lasts until the test ends, when what
// it changed is put back; one this system or this account cannot make skips the test, saying why.
// SystemFile finds a file the system owns that this account may not write.
// GiveGroup gives a name to a group, as the admin group, with no os.Chown in the test.
// A plant for RootOnly that must reach the program cannot be made on Windows without elevation.
// A test probes for it with trust.Path(dir, trust.Folder, trust.Who{RootOnly: true})
// on the folder it would plant in, and skips where that refuses. That probe uses no GOOS.
// A test imports this package itself: no other module forwards its names.
// It imports the standard library, golang.org/x/sys and trust.
package trusttest

import (
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/birdtable/go-files/trust"
)

// Who is whom Loosen lets in, and to do what.
type Who int

// The accounts Loosen lets in, each to read or to write.
const (
	// GroupReads lets the name's group read it on Unix, and Users, S-1-5-32-545, on Windows.
	GroupReads Who = iota + 1
	// GroupWrites lets the name's group write it on Unix, and Users on Windows.
	GroupWrites
	// OthersRead lets every other account read it on Unix, and Everyone, S-1-1-0, on Windows.
	OthersRead
	// OthersWrite lets every other account write it on Unix, and Everyone on Windows.
	OthersWrite
)

// errNoLists is a Windows volume that keeps no access lists, as FAT, where no list is changed.
var errNoLists = errors.New("its volume keeps no access lists, so no account is let in or kept out by one")

// Loosen lets who read or write path until the test ends, when path's mode, or on Windows its
// access list, is put back as it was. On Unix it adds the one mode bit, as os.Chmod does; on
// Windows it adds an allow entry for Users or Everyone, of the rights to read or to write a file,
// to the list of the file or folder path names. Each follows a link at path. trust then refuses
// the name as a Key or a PrivateFolder, and, let written, as any kind. On a Windows volume that
// keeps no access lists, as FAT, the test is skipped; any other failure fails it.
func Loosen(t testing.TB, path string, who Who) {
	t.Helper()
	if who < GroupReads || who > OthersWrite {
		t.Fatalf("trusttest.Loosen %s: %d is no Who", path, who)
	}
	put, err := loosen(path, who)
	if errors.Is(err, errNoLists) {
		t.Skipf("trusttest.Loosen %s: %v", path, err)
	}
	if err != nil {
		t.Fatalf("trusttest.Loosen %s: %v", path, err)
	}
	t.Cleanup(putBack(t, "Loosen", path, put))
}

// Deny makes path unreadable to this account until the test ends, when its mode, or on Windows
// its access list, is put back as it was. On Unix it clears every mode bit, as os.Chmod does; on
// Windows it sets an empty access list that takes nothing from the folder above, which lets no
// account but the owner, which may still change the list, open it. Each follows a link at path.
// Where this account still opens path to read, as root does on Unix and an account an access list
// lets in does on macOS, it is put back at once and the test is skipped, saying so; so is a
// Windows volume that keeps no access lists.
func Deny(t testing.TB, path string) {
	t.Helper()
	denyBy(t, path, openRead)
}

// denyBy is Deny, learning by open whether this account still opens path: openRead, or in
// trusttest's own tests one that fails for a reason other than the denial, or one that opens.
func denyBy(t testing.TB, path string, open func(string) (*os.File, error)) {
	t.Helper()
	put, err := deny(path)
	if errors.Is(err, errNoLists) {
		t.Skipf("trusttest.Deny %s: %v", path, err)
	}
	if err != nil {
		t.Fatalf("trusttest.Deny %s: %v", path, err)
	}
	f, err := open(path)
	if err == nil || !errors.Is(err, fs.ErrPermission) {
		opened := err == nil
		if opened {
			if cerr := f.Close(); cerr != nil {
				t.Errorf("trusttest.Deny %s: closing the name opened to learn whether it could be: %v", path, cerr)
			}
		}
		if perr := put(); perr != nil {
			t.Fatalf("trusttest.Deny %s: putting back what it changed: %v", path, perr)
		}
		if opened {
			t.Skipf("trusttest.Deny %s: this account opens it still, as root does on Unix and an account a list lets in, so it cannot be kept out", path)
		}
		t.Fatalf("trusttest.Deny %s: an open of it, made unreadable, failed with %v, not a refusal of this account", path, err)
	}
	t.Cleanup(putBack(t, "Deny", path, put))
}

// putBack is the cleanup that calls put, which puts back what op changed at path, and fails t
// when it cannot. A name the test removed has nothing to put back.
func putBack(t testing.TB, op, path string, put func() error) func() {
	t.Helper()
	return func() {
		if err := put(); err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("trusttest.%s %s: putting back what it changed: %v", op, path, err)
		}
	}
}

// Perm fails the test unless path, followed where it is a link, has the mode this system keeps
// for want as os.Chmod gives it: want itself on Unix, and on Windows, which keeps the owner write
// bit alone as the read-only attribute, 0o444 for a want without 0o200 and 0o666 for one with it,
// a folder adding 0o111. Where want lets its group and others nothing, path must be private to
// this account too, as trust.Held judges a Key, or a PrivateFolder, on the name opened to read: of
// this account or of root, Administrators or SYSTEM, and no other account let read or write it by
// its mode, a Linux access list's mask, or a macOS or Windows access list. A private want must
// then let its owner read; a want with bits beyond the nine fails the test. Held is given no Who,
// so on Linux a private want fails on a disk trust refuses until Who.Mounts names it, one mounted
// nosuid, or noexec without nosuid, whatever path's mode: Perm has no Who to name the mount, and a
// test that needs a private name keeps it on a disk without those flags, as a GitHub runner's work
// folder is.
// Perm, and Deny, open path with no wait for a pipe's other end, so a pipe at path fails or skips
// the test, never hangs it.
func Perm(t testing.TB, path string, want fs.FileMode) {
	t.Helper()
	if want&^fs.ModePerm != 0 {
		t.Fatalf("trusttest.Perm %s: want %v holds bits beyond the nine permission bits, which no system keeps by os.Chmod alike", path, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("trusttest.Perm %s: %v", path, err)
	}
	if got, k := info.Mode().Perm(), kept(want, info.IsDir()); got != k {
		t.Errorf("trusttest.Perm %s: its mode is %v, and want %v, as this system keeps %v", path, got, k, want)
	}
	if want&0o077 != 0 {
		return
	}
	if want&0o400 == 0 {
		t.Fatalf("trusttest.Perm %s: want %v lets its owner not read, so whether it is private is not read on it", path, want)
	}
	kind := trust.Key
	if info.IsDir() {
		kind = trust.PrivateFolder
	}
	f, err := openRead(path)
	if err != nil {
		t.Fatalf("trusttest.Perm %s: %v", path, err)
	}
	defer f.Close() //nolint:errcheck // a file only judged
	if err := trust.Held(f, kind, trust.Who{}); err != nil {
		t.Errorf("trusttest.Perm %s: it is not private to this account: %v", path, err)
	}
}

// Bound is how Bind mounts the folder it binds.
type Bound int

// The mounts Bind makes, each as a program in the field mounts an account's stick.
const (
	// Nosuid mounts it nosuid, as udisks, fuse2fs or fstab's user mount an account's stick.
	Nosuid Bound = iota + 1
	// NoexecWithoutNosuid mounts it noexec and nodev without nosuid, as usbmount mounts one.
	NoexecWithoutNosuid
)

// Bind gives a test, on Linux 5.12 or later, a mount with how's flag for trust to judge, with no
// root: entry, a folder of Who.Mounts, and dir, a folder of this account's on that mount.
// It binds a folder of the test's temp folder named trusttest-bind, which must sit
// on a disk whose flags trust judges, over entry, the first of /mnt and /srv that is a folder real
// root made, which trust passes with Who.RootOnly and Who.Overflow, whatever it holds, and that no
// Bind holds. It sets how's flag on the mount before it places it, keeping each flag the kernel
// locks on the disk's own mount, by open_tree, mount_setattr and move_mount, which Linux 5.12 has.
// It mounts only in a user namespace other than the first, whose /proc/self/uid_map is not the map
// of every id to itself, so the mount is in a mount namespace that user namespace owns and never
// the system's, as unshare --user --map-current-user --mount --keep-caps makes; there it hides what
// entry holds, from that namespace alone, until it unmounts at the test's end. Parallel tests may
// each call it, in one test binary or in several that share one namespace, as go test ./... run in
// one unshare does: each gets an entry no other holds, or skips. Binds of several processes are
// judged by /proc/self/mountinfo, with no lock between them and no wait: an entry where a Bind's
// mount stands, one whose root is a folder named trusttest-bind, is held, and where two Binds mount
// over one entry at once, the earliest keeps it and the later undoes its own mount and tries the
// next. A Bind's mount left by a process that ended without its unmount holds its entry while the
// namespace lasts. With more Binds at once than entries, the extra ones skip, so a caller that
// wants none to skip runs them one at a time. A caller that mixes Nosuid and
// NoexecWithoutNosuid across processes runs those one at a time too: of two racing over one entry,
// the one that keeps it may judge the other's mount, with the other flag and an empty dir, until
// that one is undone. In such a namespace this account's files are its own and real
// root's show as the overflow account: a test walks with trust.Who{Overflow: true, Mounts:
// []string{entry}}, which passes a name in dir that trust refuses with ErrMount without Mounts.
// Bind skips the test, saying why, off Linux; where it may not mount (in the first user namespace,
// root's on the host among it, or one that maps root, as unshare --map-root-user's does, or where
// mount is refused, as without --keep-caps, or in go-proc's testenv.Contain, which takes away the
// capability to mount before its tests run, or on a kernel before Linux 5.12, or where a seccomp
// profile older than the kernel refuses open_tree, mount_setattr or move_mount, the skip naming the
// call refused, open_tree before Linux 5.2 and mount_setattr from 5.2 to 5.11, and the errno,
// ENOSYS or EPERM); where neither /mnt nor /srv qualifies or is free; and where the temp folder's
// disk is one whose flags trust does not judge, as a tmpfs /tmp, or, for NoexecWithoutNosuid, one
// mounted nosuid, which no namespace lifts. Any other failure fails the test.
func Bind(t testing.TB, how Bound) (entry, dir string) {
	t.Helper()
	if how != Nosuid && how != NoexecWithoutNosuid {
		t.Fatalf("trusttest.Bind: %d is no Bound", how)
	}
	return bind(t, how, []string{"/mnt", "/srv"}, hook{})
}

// hook is what a test of the package changes in a Bind, read only under testing.Testing(): blind
// drops the checks before the mount, as a Bind's whose read came before another's mount, and
// between runs after the mount and before the check after it. between must return: it runs with
// Bind's lock let go and its mount placed, so one that ends its goroutine, as t.FailNow does,
// would leave the mount and unlock the lock again, which Go stops as a fatal error.
type hook struct {
	blind   bool
	between func()
}

package trust

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"

	"github.com/birdtable/go-files/internal/procfs"
	"golang.org/x/sys/unix"
)

// mountOf is n, the facts of the name path, with the account that mounted its file system when
// that is a FUSE one: its server reports any owner and mode it likes, so a name on one another
// account mounted, with allow_other, never passes on what it reports, below folders that pass or
// not. The name is opened with O_PATH, which reads nothing and needs no right on
// it, and held to the file lstat read by its device and inode, so the file system judged is the
// one its facts came from, kept mounted while it is judged; another file there is errMoved, and
// is read again.
func mountOf(n node, path string) (node, error) {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return node{}, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	defer unix.Close(fd) //nolint:errcheck // an O_PATH descriptor holds nothing to flush
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return node{}, &fs.PathError{Op: "fstat", Path: path, Err: err}
	}
	if bits(st.Dev) != n.id.vol || bits(st.Ino) != n.id.idx {
		return node{}, errMoved
	}
	return fsOf(n, path, fd)
}

// mountOfFile is mountOf for the file f is open on, read by its descriptor.
func mountOfFile(n node, f *os.File) (node, error) {
	c, err := f.SyscallConn()
	if err != nil {
		return node{}, err
	}
	var got node
	if cerr := c.Control(func(fd uintptr) { got, err = fsOf(n, f.Name(), int(fd)) }); cerr != nil {
		return node{}, cerr
	}
	return got, err
}

// fsOf is n with what its file system tells of it, by fstatfs: on a FUSE file system, its mounter,
// the account its mount in /proc/self/mountinfo names, found by n's device, which no other file
// system has while fd holds this one; on NFS, its NFSv4 list; on ZFS, whether it carries one.
func fsOf(n node, path string, fd int) (node, error) {
	var sf unix.Statfs_t
	if err := unix.Fstatfs(fd, &sf); err != nil {
		return node{}, &fs.PathError{Op: "fstatfs", Path: path, Err: err}
	}
	n.fsType, n.typed = uint32(sf.Type), true //nolint:gosec // G115: a magic number is the low 32 bits of f_type
	n.nosuid, n.noexec = sf.Flags&unix.ST_NOSUID != 0, sf.Flags&unix.ST_NOEXEC != 0
	return typedOf(n, path, fd, readMountinfo)
}

// typedOf is fsOf's n, its fsType read, with what that type tells of it, so a row may give the
// type its disk has not: for ext2/3/4, xfs and f2fs, whether n's device, which is
// fd's, has major 0; for any other type but 0, and for those on major 0, so a 9p share of a disk
// is judged as 9p, the type mountinfo, which read gives, gives its mount. plainRefused holds the
// type to them. A mount whose type is not read is refused there, so nothing more is read
// of it, and one mount answers one way.
func typedOf(n node, path string, fd int, read func() ([]byte, error)) (node, error) {
	n.anon = byDevice(n.fsType) && unix.Major(n.id.vol) == 0
	if n.fsType != 0 && (n.anon || !byDevice(n.fsType)) {
		var err error
		if n.kind, err = kindOf(fd, read); err != nil {
			return node{}, &fs.PathError{Op: "read the mount of", Path: path, Err: err}
		}
		if n.kind == "" {
			return n, nil
		}
	}
	if n.fsType == unix.NFS_SUPER_MAGIC {
		return nfs4Of(n, path, fd)
	}
	if n.fsType == zfsMagic && n.typ != fs.ModeSymlink {
		_, err := xattrOf(fd, "system.nfs4_acl_xdr")
		return zfsOf(n, path, err)
	}
	if n.fsType != unix.FUSE_SUPER_MAGIC {
		return n, nil
	}
	b, err := read()
	if n, err = mounterOf(n, path, devName(n.id.vol), b, err); err != nil {
		return node{}, err
	}
	own, mount := userNS()
	n.mounter = ofInitialNS(n.mounter, own, mount)
	return n, nil
}

// maxProc is the most read of one file under /proc: a mountinfo of tens of thousands of mounts is
// well under it, and a file past it, as one that does not end, is errProcLong.
const maxProc = 4 << 20

// errProcLong is a file under /proc longer than maxProc.
var errProcLong = errors.New("is longer than any file trust reads under /proc")

// readProc reads the file path under /proc, up to maxProc, refused with errNotProc where fstatfs on
// the file it opened says it is not on procfs.
func readProc(path string) ([]byte, error) { return readProcUpTo(path, maxProc) }

// readProcUpTo is readProc with the bound limit. Another file of procfs mounted or linked at path
// passes this check, a limit the README states: only mountinfo is held by openat2.
func readProcUpTo(path string, limit int64) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	var sf unix.Statfs_t
	if err := unix.Fstatfs(fd, &sf); err != nil || uint32(sf.Type) != unix.PROC_SUPER_MAGIC { //nolint:gosec // G115: a magic number is the low 32 bits of f_type
		unix.Close(fd) //nolint:errcheck,gosec // G104: a file not kept
		return nil, &fs.PathError{Op: "fstatfs", Path: path, Err: cmp.Or(err, errNotProc)}
	}
	return readUpTo(fd, path, limit)
}

// readMountinfo reads /proc/self/mountinfo, up to maxProc, by readMountinfoUpTo.
func readMountinfo() ([]byte, error) { return readMountinfoUpTo(maxProc) }

// readMountinfoUpTo reads self/mountinfo from procRoot by openat2, crossing no mount and following
// no magic link nor a link at its end, so it is this procfs's own, and no other file of procfs
// mounted or linked there, as a process's own cmdline, which an owner of a joined mount namespace
// may bind over it. A /proc not procfs's root, and a
// mountinfo so not held, are errNotProc, as is ENOSYS (a seccomp filter, or below the 5.8 floor), so
// kindOf reads no type and every type held to its mount is ErrMount; EPERM makes each an error.
func readMountinfoUpTo(limit int64) ([]byte, error) {
	const path = "/proc/self/mountinfo"
	root, err := procRoot()
	if err != nil {
		return nil, err
	}
	defer unix.Close(root) //nolint:errcheck // an O_PATH descriptor holds nothing to flush
	fd, err := procfs.Beneath(root, "self/mountinfo", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC)
	if err != nil {
		return nil, &fs.PathError{Op: "openat2", Path: path, Err: notProcOpen(err)}
	}
	return readUpTo(fd, path, limit)
}

// notProcOpen is err, openat2's on self/mountinfo, as errNotProc where it shows the file is not
// this procfs's own: ENOSYS where openat2 is refused or absent, EXDEV or ELOOP for a mount or link
// there, and ENOENT where this process is not in /proc's pid namespace, so self names nothing. The errno is
// shown and never matched, so ENOENT reads as no name-not-there of the name walked.
func notProcOpen(err error) error {
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EXDEV) || errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("%w: %v", errNotProc, err) //nolint:errorlint // the errno is shown, never matched
	}
	return err
}

// readUpTo reads the file fd, named path, and closes it: one longer than limit is errProcLong.
func readUpTo(fd int, path string, limit int64) ([]byte, error) {
	f := os.NewFile(uintptr(fd), path)
	defer f.Close() //nolint:errcheck // a file only read
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(b)) > limit {
		return nil, &fs.PathError{Op: "read", Path: path, Err: errProcLong}
	}
	return b, err
}

// procRoot is /proc opened O_PATH and held to a procfs's root, by fstatfs and its inode, so a file
// system mounted there, or a folder of a procfs bound there, is errNotProc: internal/procfs's Root.
func procRoot() (int, error) { return procfs.Root("/proc") }

// devName is the device dev as mountinfo writes it, "<major>:<minor>".
func devName(dev uint64) string {
	return strconv.FormatUint(uint64(unix.Major(dev)), 10) + ":" + strconv.FormatUint(uint64(unix.Minor(dev)), 10)
}

// errNoMountID is a descriptor whose mount statx does not number, as before Linux 5.8, which has
// no STATX_MNT_ID. /proc/self/fdinfo is not read for it: an owner of a namespace this process
// joined may bind its own over it.
var errNoMountID = errors.New("no mount number is read for it")

// mountRoot is whether entry, a name that is no link, is the root of a mount, by its mount and
// never its device, as a disk bound over a folder of the same disk keeps its device: statx's
// STATX_ATTR_MOUNT_ROOT, which the kernel answers for every file system since Linux 5.8, the
// release that first numbers mounts by statx too, so no mount number tells what it does not. A
// kernel before 5.8 tells no mount, and the entry is refused.
func mountRoot(entry, _ string) (bool, error) {
	var e unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, entry, unix.AT_SYMLINK_NOFOLLOW, unix.STATX_TYPE, &e); err != nil {
		return false, &fs.PathError{Op: "statx", Path: entry, Err: err}
	}
	if e.Attributes_mask&unix.STATX_ATTR_MOUNT_ROOT == 0 {
		return false, errors.New("statx answers no STATX_ATTR_MOUNT_ROOT, as before Linux 5.8")
	}
	return e.Attributes&unix.STATX_ATTR_MOUNT_ROOT != 0, nil
}

// mountID is the number of the mount fd is on, by statx.
func mountID(fd int) (string, error) {
	var sx unix.Statx_t
	if unix.Statx(fd, "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_MNT_ID, &sx) == nil && sx.Mask&unix.STATX_MNT_ID != 0 {
		return strconv.FormatUint(sx.Mnt_id, 10), nil
	}
	return "", errNoMountID
}

// kindOf is the type mountinfo, which read gives, gives the mount fd is on, found by its mount
// number, as a btrfs subvolume's device is not its mount's there. A mount not numbered, as before
// Linux 5.8, or a mountinfo not held to procfs gives "", so plainRefused refuses the name.
func kindOf(fd int, read func() ([]byte, error)) (string, error) {
	id, err := mountID(fd)
	var b []byte
	if err == nil {
		b, err = read()
	}
	if errors.Is(err, errNotProc) || errors.Is(err, errNoMountID) {
		return "", nil
	}
	return mountKind(string(b), id), err
}

// nfs4Of is n with the NFSv4 list of the name fd holds, read by xattrOf so it is that name's.
// None kept is no list, and a mount that cannot show one, as NFSv3,
// is nfsUnshown; a link's own is not read, as Linux follows a link whatever it
// holds; any other error, as a name this account may not read, leaves the name unjudged.
func nfs4Of(n node, path string, fd int) (node, error) {
	if n.typ == fs.ModeSymlink {
		return n, nil
	}
	b, err := xattrOf(fd, "system.nfs4_acl")
	if err == nil && len(b) > 0 {
		return nfs4List(n, b), nil
	}
	n.nfsUnshown = errors.Is(err, unix.EOPNOTSUPP)
	if err == nil || n.nfsUnshown || errors.Is(err, unix.ENODATA) {
		return n, nil
	}
	return node{}, &fs.PathError{Op: "getxattr", Path: path, Err: err}
}

// zfsOf is n, a ZFS name, by err, xattrOf's answer for its system.nfs4_acl_xdr, the NFSv4 list
// TrueNAS SCALE enforces: one kept is zfsList, ENODATA or EOPNOTSUPP is none, since only the held
// file gives those, and any other error leaves the name unjudged. A link's
// own is not read, as for NFS.
func zfsOf(n node, path string, err error) (node, error) {
	n.zfsList = err == nil
	if err == nil || errors.Is(err, unix.ENODATA) || errors.Is(err, unix.EOPNOTSUPP) {
		return n, nil
	}
	return node{}, &fs.PathError{Op: "getxattr", Path: path, Err: err}
}

// errNotReopened is a name held with O_PATH that is not a file or a folder, so it is not opened to
// read its extended attributes: a device's open may act, and a socket's fails.
var errNotReopened = errors.New("is not a file or a folder, so it is not opened to read its list")

// errNotHeldFile is a name reopened through /proc/self/fd that is not the file held open: an owner
// of this mount namespace may mount over that path and answer another file there.
var errNotHeldFile = errors.New("reopened, is not shown to be the file held open, by its mount, device and inode")

// xattrOf is the extended attribute name of the file or folder fd holds with O_PATH, read with
// fgetxattr. Neither fgetxattr nor Linux 6.13's getxattrat takes an O_PATH descriptor, as both get
// it by fdget, and any path to it may be mounted over; so fd is reopened, to be read, through
// /proc/self/fd, and the descriptor opened is held to fd by its mount number, device and inode
// before it is asked; each error before then is errNotHeldFile, which matches no errno, so the path
// never answers ENODATA or EOPNOTSUPP for the held file. A name this account may not
// read is not opened, and is an error.
func xattrOf(fd int, name string) ([]byte, error) {
	notHeld := func(err error) ([]byte, error) { return nil, fmt.Errorf("%w: %v", errNotHeldFile, err) } //nolint:errorlint // the errno is shown, never matched
	var held unix.Stat_t
	if err := unix.Fstat(fd, &held); err != nil {
		return notHeld(err)
	}
	flags := unix.O_RDONLY | unix.O_NONBLOCK | unix.O_NOCTTY | unix.O_CLOEXEC
	switch held.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		flags |= unix.O_DIRECTORY
	case unix.S_IFREG:
	default:
		return nil, errNotReopened
	}
	r, err := unix.Open("/proc/self/fd/"+strconv.Itoa(fd), flags, 0)
	if err != nil {
		return notHeld(err)
	}
	defer unix.Close(r) //nolint:errcheck // a descriptor only read of
	var st unix.Stat_t
	if err := unix.Fstat(r, &st); err != nil {
		return notHeld(err)
	}
	heldID, err := mountID(fd)
	if err != nil {
		return notHeld(err)
	}
	id, err := mountID(r)
	if err != nil {
		return notHeld(err)
	}
	if id != heldID || st.Dev != held.Dev || st.Ino != held.Ino {
		return nil, errNotHeldFile
	}
	size, err := unix.Fgetxattr(r, name, nil)
	if err != nil || size == 0 {
		return nil, err
	}
	b := make([]byte, size)
	if size, err = unix.Fgetxattr(r, name, b); err != nil {
		return nil, err
	}
	return b[:size], nil
}

// The ioctls x/sys does not name yet, Linux 6.11's PIDFD_GET_MNT_NAMESPACE and
// PIDFD_GET_USER_NAMESPACE, _IO(0xFF, 3) and _IO(0xFF, 9), built on NS_GET_NSTYPE's direction
// bits, as _IO's differ by architecture. Whatever a call answers, its namespace's type is checked
// by NS_GET_NSTYPE before it is read.
const (
	pidfdGetMntNS  = unix.NS_GET_NSTYPE&^0xffff | 0xff03
	pidfdGetUserNS = unix.NS_GET_NSTYPE&^0xffff | 0xff09
)

// userNS is this process's user namespace and its mount namespace's owner, read by NS_GET_USERNS;
// one not read is the zero nsFile. Both come from pidfdNS, which reads nothing under /proc, and
// only where this kernel has no pidfd namespace calls from procNS.
func userNS() (own, mount nsFile) {
	user, mnt, ok := pidfdNS()
	if !ok {
		user, mnt = procNS()
	}
	defer unix.Close(user) //nolint:errcheck // a namespace descriptor holds nothing to flush
	defer unix.Close(mnt)  //nolint:errcheck // as user
	own = nsOf(user, unix.CLONE_NEWUSER)
	if nsOf(mnt, unix.CLONE_NEWNS) == (nsFile{}) {
		return own, mount
	}
	ns, err := unix.IoctlRetInt(mnt, unix.NS_GET_USERNS)
	if err != nil {
		return own, mount
	}
	defer unix.Close(ns) //nolint:errcheck // as user
	return own, nsOf(ns, unix.CLONE_NEWUSER)
}

// pidfdNS is this process's user and mount namespaces, from a pidfd of its own by Linux 6.11's
// calls, and whether both were read; -1 is one not read.
func pidfdNS() (user, mnt int, ok bool) {
	pid, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		return -1, -1, false
	}
	defer unix.Close(pid) //nolint:errcheck // a pidfd holds nothing to flush
	return nsOfPidfd(pid, unix.IoctlRetInt)
}

// nsOfPidfd is pidfdNS's two calls on pid by ioctl. A call that fails answers no descriptor, though
// x/sys answers 0 with its error, so only what a call opened is closed.
func nsOfPidfd(pid int, ioctl func(fd int, req uint) (int, error)) (user, mnt int, ok bool) {
	user, err := ioctl(pid, pidfdGetUserNS)
	if err != nil {
		return -1, -1, false
	}
	if mnt, err = ioctl(pid, pidfdGetMntNS); err != nil {
		unix.Close(user) //nolint:errcheck,gosec // G104: a namespace descriptor not kept
		return -1, -1, false
	}
	return user, mnt, true
}

// procNS is this process's user and mount namespaces opened by /proc/self/ns; -1 is one not opened.
func procNS() (user, mnt int) {
	user, err := unix.Open("/proc/self/ns/user", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		user = -1
	}
	mnt, err = unix.Open("/proc/self/ns/mnt", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		mnt = -1
	}
	return user, mnt
}

// nsOf is the namespace fd holds, where fstatfs says it is on nsfs and NS_GET_NSTYPE that it is a
// namespace of the type typ, and the zero nsFile otherwise: a file mounted over /proc/self/ns is
// not taken for one.
func nsOf(fd, typ int) nsFile {
	var sf unix.Statfs_t
	var st unix.Stat_t
	if fd < 0 || unix.Fstatfs(fd, &sf) != nil || uint32(sf.Type) != unix.NSFS_MAGIC || unix.Fstat(fd, &st) != nil { //nolint:gosec // G115: a magic number is the low 32 bits of f_type
		return nsFile{}
	}
	if t, err := unix.IoctlRetInt(fd, unix.NS_GET_NSTYPE); err != nil || t != typ {
		return nsFile{}
	}
	return nsFile{bits(st.Dev), bits(st.Ino)}
}

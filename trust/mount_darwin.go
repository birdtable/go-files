package trust

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

// mountOf is n, the facts of the name path, with the folder its volume is mounted on when that
// volume ignores ownership (MNT_IGNORE_OWNERSHIP, as an external disk usually is): there macOS
// shows every caller as the owner of every name and gives it the owner's rights, so no owner read
// there says who can change the name, and with the account that mounted it, its
// f_owner, as ignored tells. statfs follows a link, so a link is read by
// the folder that holds it, which is on the link's volume; a name cannot be renamed onto another
// volume, and only whoever may change its folder, judged before it, can mount one at it.
func mountOf(n node, path string) (node, error) {
	at := path
	if n.typ == fs.ModeSymlink {
		at = filepath.Dir(path)
	}
	var st unix.Statfs_t
	if err := unix.Statfs(at, &st); err != nil {
		return node{}, &fs.PathError{Op: "statfs", Path: at, Err: err}
	}
	return ignored(n, &st), nil
}

// mountRoot is whether entry is a mount's root, by a device other than parent's, as each macOS
// volume has its own.
func mountRoot(entry, parent string) (bool, error) { return onOtherDevice(entry, parent) }

// mountOfFile is mountOf for the file f is open on, read by its descriptor.
func mountOfFile(n node, f *os.File) (node, error) {
	c, err := f.SyscallConn()
	if err != nil {
		return node{}, err
	}
	var st unix.Statfs_t
	if cerr := c.Control(func(fd uintptr) { err = unix.Fstatfs(int(fd), &st) }); cerr != nil {
		return node{}, cerr
	}
	if err != nil {
		return node{}, &fs.PathError{Op: "fstatfs", Path: f.Name(), Err: err}
	}
	return ignored(n, &st), nil
}

// ignored is n with what st, the statfs of its volume, tells: whether it ignores ownership, and
// the account that mounted it, f_owner, as a disk image or FUSE volume another account attached
// reports any owner and mode it likes.
func ignored(n node, st *unix.Statfs_t) node {
	on := unix.ByteSliceToString(st.Mntonname[:])
	if st.Flags&unix.MNT_IGNORE_OWNERSHIP != 0 {
		n.ownersIgnored = on
	}
	n.mounter, n.mountedOn = "uid:"+strconv.FormatUint(uint64(st.Owner), 10), on
	return n
}

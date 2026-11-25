package trust

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// unnamed is the mounter of a FUSE file system whose mounter is not told: an account trust never
// trusts, so a name there is refused.
const unnamed = "an account not named"

// fields is line's fields, a line of Linux's /proc/self/mountinfo, split on the one byte 0x20 the
// kernel writes between them and on no other. The kernel escapes a space, tab, newline or backslash
// within a field, and no other byte, so a field may hold a \r, \v, \f, U+0085 or U+00A0, which
// strings.Fields would cut it at: a FUSE mount's source, subtype or mount point, which its mounter
// writes, could then spell fields of its own, a user_id= of another account's or a virtiofs type.
func fields(line string) []string {
	return strings.Split(strings.TrimSuffix(line, "\n"), " ")
}

// fuseMounter is who mounted the FUSE file system of the device dev, "<major>:<minor>", read from
// mountinfo, the text of Linux's /proc/self/mountinfo, and the folder it is mounted on. It is
// judged on every system, so its tests run on every system. A FUSE mount names its mounter
// by user_id=, among its file system's options, and the account is answered as an owner is written.
// virtiofs, on the same kernel driver, only root mounts, and has no mounter. Any other type with no
// user_id=, or a mount mountinfo does not hold, is unnamed's, and so is refused.
func fuseMounter(mountinfo, dev string) (who, on string) {
	for line := range strings.Lines(mountinfo) {
		f := fields(line)
		if len(f) < 7 || f[2] != dev {
			continue
		}
		sep := slices.Index(f[6:], "-") + 6
		if sep < 6 || len(f) < sep+4 {
			return unnamed, f[4]
		}
		if f[sep+1] == "virtiofs" {
			return "", f[4]
		}
		for o := range strings.SplitSeq(f[sep+3], ",") {
			if id, ok := strings.CutPrefix(o, "user_id="); ok {
				return "uid:" + id, f[4]
			}
		}
		return unnamed, f[4]
	}
	return unnamed, ""
}

// initUserNS is the inode of Linux's initial user namespace, PROC_USER_INIT_INO.
const initUserNS = 0xEFFFFFFD

// nsFile is a namespace file's device and inode.
type nsFile struct{ dev, ino uint64 }

// ofInitialNS is who, fuseMounter's, where this process's user namespace own and its mount
// namespace's owner mount are one file and the initial one, and unnamed otherwise, as user_id= is
// numbered in the mounter's namespace. One not read is the zero nsFile.
func ofInitialNS(who string, own, mount nsFile) string {
	if who == "" || own == mount && own.ino == initUserNS {
		return who
	}
	return unnamed
}

// errNotProc is a file read under /proc that is not held to this process's procfs: an owner of this
// mount namespace may mount over /proc, or bind or link another file of a procfs at mountinfo,
// and serve what it likes.
var errNotProc = errors.New("is not held to procfs's own file, so what it holds is not the kernel's")

// mounterOf is n, a name on a FUSE file system of the device dev, with its mounter and the folder
// it is mounted on, from b, Linux's /proc/self/mountinfo as read with err, before ofInitialNS. A
// mountinfo not on procfs names no mounter, so the name is refused; any other error leaves it
// unjudged.
func mounterOf(n node, path, dev string, b []byte, err error) (node, error) {
	switch {
	case errors.Is(err, errNotProc):
		n.mounter = unnamed
	case err != nil:
		return node{}, err
	default:
		n.mounter, n.mountedOn = fuseMounter(string(b), dev)
	}
	if n.mountedOn == "" {
		n.mountedOn = path
	}
	return n, nil
}

// zfsMagic is ZFS's statfs type, which OpenZFS sets and no Linux header names.
const zfsMagic = 0x2FC12FC1

// fuseMagic is FUSE's statfs type, which the kernel sets for every FUSE server.
const fuseMagic = 0x65735546

// kernelTyped is whether t is a statfs type the kernel answers itself, whatever backs the mount:
// tmpfs, ramfs, overlayfs, FUSE or NFS, so its mount's type is not read again from mountinfo.
func kernelTyped(t uint32) bool {
	return slices.Contains([]uint32{0x01021994, 0x858458F6, 0x794C7630, fuseMagic, 0x6969}, t)
}

// ninePMagic is 9p's own statfs type, V9FS_MAGIC. A 9P2000.L client answers fstatfs with its
// server's type, whatever it is, so a host's ext4, tmpfs or overlayfs shared to a guest reads as
// that type there.
const ninePMagic = 0x01021997

// byDevice is whether t is ext2/3/4's, xfs's or f2fs's type, which passes on a device whose major
// is not 0, as no 9p mount's is, so its mount's type is not read.
func byDevice(t uint32) bool { return slices.Contains([]uint32{0xEF53, 0x58465342, 0xF2F52010}, t) }

// diskKinds is the names Linux's mountinfo gives the mounts of t, btrfs's or ZFS's type, or nil.
func diskKinds(t uint32) []string {
	return map[uint32][]string{0x9123683E: {"btrfs"}, zfsMagic: {"zfs"}}[t]
}

// agrees is whether kind, the type mountinfo gives a name's mount, is t's, its fstatfs type: for a
// type of diskKinds, one of its names; any other type agrees with any kind read.
func agrees(t uint32, kind string) bool {
	names := diskKinds(t)
	return names == nil || slices.Contains(names, kind)
}

// mountKind is the type mountinfo, the text of Linux's /proc/self/mountinfo, gives the mount it
// numbers id in its first field, or "" where it holds none. It is judged on every system.
func mountKind(mountinfo, id string) string {
	for line := range strings.Lines(mountinfo) {
		f := fields(line)
		if len(f) < 7 || f[0] != id {
			continue
		}
		if sep := slices.Index(f[6:], "-") + 6; sep >= 6 && sep+1 < len(f) {
			return f[sep+1]
		}
		return ""
	}
	return ""
}

// plainRefused is why n, on a file system whose type Linux's fstatfs read, is on a mount whose
// owners trust does not judge, or "". The kernel keeps the owners of tmpfs, ramfs and
// overlayfs, which only root mounts outside a user namespace, and of ext2/3/4, xfs, btrfs, f2fs, ZFS
// and who.FileSystems, which a disk backs; FUSE and NFS keep their own rules. A type fstatfs read
// on Linux, typed, is held to its mount: ext2/3/4, xfs and f2fs by a device whose major is not 0,
// any other by kind, its mount's in mountinfo, which must be read and agree, and a mount mountinfo
// calls 9p is judged as 9p's type, refused unless who.FileSystems names it; 0 never passes there. A
// disk mounted nosuid, as fusermount, udisks and fstab's user mount an account's, and a FUSE mount
// root's helper made nosuid, as udisks' ntfs-3g and exfat-fuse are, pass on a folder of who.Mounts
// alone, which h reads as the device it is on; so does either mounted noexec without
// nosuid, as usbmount mounts an account's stick, where no setuid program the volume holds can run
// and the reason to pass a mount with setuid is gone. A ZFS name with TrueNAS
// SCALE's list is refused.
func plainRefused(n node, who Who, h host) string {
	mounted := func(m string) bool {
		id, err := h.ident(m)
		return filepath.IsAbs(m) && err == nil && id.vol == n.id.vol
	}
	t, nine := n.fsType, n.typed && n.kind == "9p"
	if nine && slices.Contains(who.FileSystems, ninePMagic) {
		t = ninePMagic
	}
	switch {
	case n.zfsList:
		return "of type 0x2fc12fc1, ZFS, where it carries an NFSv4 list, system.nfs4_acl_xdr, that trust does not read"
	case n.typed && n.fsType == 0:
		return "of type 0, as fstatfs answers it, which a 9p server may: trust does not judge its owners"
	case nine && t != ninePMagic:
		return fmt.Sprintf("of type %#x by fstatfs, on a mount mountinfo gives the type \"9p\", whose server answers fstatfs as it likes, and no file system type named as trusted is 9p's, %#x", n.fsType, ninePMagic)
	case n.typed && byDevice(t) && n.anon:
		return fmt.Sprintf("of type %#x by fstatfs, a disk's, on a device of major 0, as a 9p mount's is and no disk's", n.fsType)
	case n.typed && !byDevice(t) && n.kind == "":
		return fmt.Sprintf("of type %#x by fstatfs, where its mount's type is not read from mountinfo, as before Linux 5.8, which numbers no mount by statx", n.fsType)
	case n.typed && !agrees(t, n.kind):
		return fmt.Sprintf("of type %#x by fstatfs, which a 9p server answers as it likes, where mountinfo gives its mount the type %q", n.fsType, n.kind)
	case t == fuseMagic && slices.Contains(h.root, n.mounter):
		return flagRefused(n, true, who, mounted)
	case t == 0 || kernelTyped(t):
		return ""
	case !byDevice(t) && diskKinds(t) == nil && !slices.Contains(who.FileSystems, t):
		return fmt.Sprintf("of type %#x, whose owners trust does not judge", n.fsType)
	}
	return flagRefused(n, false, who, mounted)
}

// flagRefused is why n, on a disk, or where fuse on a FUSE mount root's helper made, is refused for
// its mount's flags, or "": nosuid, or noexec without nosuid, on no folder of who.Mounts, which
// mounted reads.
func flagRefused(n node, fuse bool, who Who, mounted func(string) bool) string {
	switch {
	case !n.nosuid && !n.noexec || slices.ContainsFunc(who.Mounts, mounted):
		return ""
	case fuse && n.nosuid:
		return "FUSE's, mounted nosuid by root, as udisks mounts an account's stick with ntfs-3g or exfat-fuse, and on no mount named as trusted"
	case fuse:
		return "FUSE's, mounted noexec without nosuid by root, as an automounter may mount an account's stick with ntfs-3g, and on no mount named as trusted"
	case n.nosuid:
		return "a disk backs, mounted nosuid as an account's stick or image is, and on no mount named as trusted"
	}
	return "a disk backs, mounted noexec without nosuid as usbmount mounts an account's stick, and on no mount named as trusted"
}

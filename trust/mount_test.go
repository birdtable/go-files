package trust

import (
	"errors"
	"fmt"
	"io/fs"
	"testing"
)

// What it costs when this fails: on a Mac, a key, program or folder on a volume mounted to ignore
// ownership, as an external disk usually is, passes Path or ReadOwned as this account's, while
// every other account is shown as its owner too and may rewrite it.
func TestANameOnAVolumeThatIgnoresOwnershipIsRefused(t *testing.T) {
	t.Parallel()
	const vol = "/Volumes/ext"
	on := func(n node) node { n.ownersIgnored = vol; return n }
	says := "mounted on " + vol + ", which ignores ownership"
	d, f, l := at("d"), at("d", "f"), at("l")
	cases := []struct {
		name  string
		nodes map[string]node
		path  string
		k     Kind
		r     refusal
	}{
		{"a key of this account's", map[string]node{f: on(file(meID, 0o600)), d: dir(meID, 0o755)}, f, Key, refusal{ErrOwnersIgnored, Fix{}, f + " is on the volume " + says}},
		{"a program of root's", map[string]node{f: on(file(rootID, 0o755)), d: dir(rootID, 0o755)}, f, Program, refusal{ErrOwnersIgnored, Fix{}, f + " is on"}},
		{"the folder it is mounted on, above a name", map[string]node{d: on(dir(meID, 0o755)), f: on(file(meID, 0o600))}, f, Key, refusal{ErrOwnersIgnored, Fix{}, d + " is on the volume " + says}},
		{"a file whose owner is not trusted, ownership first", map[string]node{f: on(file(otherID, 0o600)), d: dir(meID, 0o755)}, f, Key, refusal{ErrOwnersIgnored, Fix{}, says}},
		{"the same key on a volume that keeps ownership", map[string]node{f: file(meID, 0o600), d: dir(meID, 0o755)}, f, Key, refusal{}},
	}
	for _, c := range cases {
		_, err := walk(c.path, c.k, Who{}, disk(c.nodes).host(meID))
		wantRefused(t, c.name, err, c.r)
		if c.r != (refusal{}) && !errors.Is(err, ErrNotOwners) {
			t.Errorf("%s: %v does not match ErrNotOwners", c.name, err)
		}
	}

	dk := disk(map[string]node{l: on(link(meID)), f: file(meID, 0o644), d: dir(meID, 0o755)})
	dk.links[l] = "d/f"
	_, err := walk(l, File, Who{}, dk.host(meID))
	wantRefused(t, "a link on it", err, refusal{ErrOwnersIgnored, Fix{}, l + " is on"})
	if len(dk.read) != 0 {
		t.Errorf("read the link %v before it was refused", dk.read)
	}

	h := disk(nil).host(meID)
	err = judgeRead(on(file(meID, 0o600)), f, meID, true, 10, h)
	wantRefused(t, "ReadOwned, of the owner asked", err, refusal{ErrOwnersIgnored, Fix{}, says})
	wantRefused(t, "ReadOwned, on a volume that keeps ownership", judgeRead(file(meID, 0o600), f, meID, true, 10, h), refusal{})
}

// What it costs when this fails: a FUSE file system or disk image another account mounted, below
// folders that pass, passes on the owner and mode it reports: root walks into libpam-tmpdir's
// /tmp/user/1000, served by uid 1000 as root's, and runs or reads what that account serves.
func TestANameOnAVolumeAnAccountNotTrustedMountedIsRefused(t *testing.T) {
	t.Parallel()
	const userID = "uid:1000"
	const sticky = fs.ModeSticky | 0o777
	u, mnt, f, l := at("tmp", "user"), at("tmp", "user", "1000"), at("tmp", "user", "1000", "f"), at("tmp", "user", "l")
	by := func(n node, who string) node { n.mounter, n.mountedOn = who, mnt; return n }
	says := " is on the volume mounted on " + mnt + " by " + userID + ", which is not trusted here"
	way := func(who string, top node, tmp fs.FileMode) map[string]node {
		return map[string]node{at("tmp"): dir(rootID, tmp), u: dir(rootID, 0o711), mnt: by(onDevice(top, 2), who), f: by(onDevice(file(rootID, 0o755), 2), who)}
	}
	cases := []struct {
		name    string
		nodes   map[string]node
		me, who string
		w       Who
		r       refusal
	}{
		{"root walks a mount another made", way(userID, dir(rootID, 0o755), sticky), rootID, userID, Who{}, refusal{ErrNotOwners, Fix{}, mnt + says}},
		{"the same walk, a mount root made", way(rootID, dir(rootID, 0o755), sticky), rootID, rootID, Who{}, refusal{}},
		{"this account walks a mount of its own", way(userID, dir(userID, 0o755), 0o755), userID, userID, Who{}, refusal{}},
		{"root alone, a mount this account made", way(userID, dir(rootID, 0o755), 0o755), userID, userID, Who{RootOnly: true}, refusal{ErrNotOwners, Fix{}, " by " + userID + ", which is not trusted"}},
		{"a mount whose mounter is not named", way(unnamed, dir(rootID, 0o755), sticky), rootID, unnamed, Who{}, refusal{ErrNotOwners, Fix{}, " by " + unnamed}},
	}
	for _, c := range cases {
		_, err := walk(f, Program, c.w, disk(c.nodes).host(c.me))
		wantRefused(t, c.name, err, c.r)
	}

	nodes := way(rootID, dir(rootID, 0o755), sticky)
	nodes[l] = by(link(rootID), userID)
	dk := disk(nodes)
	dk.links[l] = "1000/f"
	_, err := walk(l, Program, Who{}, dk.host(rootID))
	wantRefused(t, "a link on it", err, refusal{ErrNotOwners, Fix{}, l + says})
	if len(dk.read) != 0 {
		t.Errorf("read the link %v before it was refused", dk.read)
	}

	h := disk(nil).host(rootID)
	key := file(userID, 0o600)
	wantRefused(t, "ReadOwned, of another's mount", judgeRead(by(key, otherID), f, userID, true, 10, h), refusal{ErrNotOwners, Fix{}, " by " + otherID})
	wantRefused(t, "ReadOwned, of the owner's own mount", judgeRead(by(key, userID), f, userID, true, 10, h), refusal{})
	wantRefused(t, "ReadOwned, of root's mount", judgeRead(by(key, rootID), f, userID, true, 10, h), refusal{})
}

// What it costs when this fails: on Linux a FUSE file system another account mounted is read as
// root's, or as nobody's, and passes; or root's virtiofs share is refused.
func TestTheMounterOfAFUSEFileSystemIsReadFromMountinfo(t *testing.T) {
	t.Parallel()
	const info = `22 1 259:2 / / rw,relatime shared:1 - ext4 /dev/nvme0n1p2 rw
41 22 0:52 / /home/a rw,nosuid,nodev,relatime shared:30 master:2 - fuse.sshfs a@b:/ rw,user_id=1000,group_id=1000,allow_other
42 22 0:53 / /mnt/disk rw,relatime - fuseblk /dev/sdb1 rw,user_id=0,group_id=0,default_permissions
43 22 0:54 / /mnt/share rw,relatime - virtiofs share rw
44 22 0:55 / /mnt/plain rw,relatime - fuse plain rw,group_id=0
46 22 0:57 / /mnt/ntfs rw,relatime - fuseblk.ntfs /dev/sdc1 rw,user_id=1000,group_id=1000
45 22 0:56 / /mnt/cut rw,relatime fuse
`
	cases := []struct{ dev, who, on string }{
		{"0:52", "uid:1000", "/home/a"},
		{"0:53", "uid:0", "/mnt/disk"},
		{"0:54", "", "/mnt/share"},
		{"0:55", unnamed, "/mnt/plain"},
		{"0:56", unnamed, "/mnt/cut"},
		{"0:57", "uid:1000", "/mnt/ntfs"},
		{"0:99", unnamed, ""},
	}
	for _, c := range cases {
		if who, on := fuseMounter(info, c.dev); who != c.who || on != c.on {
			t.Errorf("the mount of %s: answered %q on %q, and want %q on %q", c.dev, who, on, c.who, c.on)
		}
	}
}

// What it costs when this fails: on Linux a name on a 9p share of a host's disk is judged as that
// disk, or every name on a disk is refused.
func TestAMountsTypeIsReadFromMountinfoByItsNumber(t *testing.T) {
	t.Parallel()
	const info = `22 1 259:2 / / rw,relatime shared:1 - ext4 /dev/nvme0n1p2 rw
41 22 0:52 / /mnt/share rw,relatime shared:30 master:2 - 9p hostshare rw,trans=virtio
42 22 0:53 /@home /home rw,relatime - btrfs /dev/sdb1 rw,subvol=/@home
45 22 0:56 / /mnt/cut rw,relatime -
46 22 0:57 / /mnt/none rw,relatime ext4
`
	for _, c := range []struct{ id, kind string }{
		{"22", "ext4"}, {"41", "9p"}, {"42", "btrfs"}, {"45", ""}, {"46", ""}, {"2", ""}, {"0:52", ""},
	} {
		if got := mountKind(info, c.id); got != c.kind {
			t.Errorf("the mount numbered %s: of type %q, and want %q", c.id, got, c.kind)
		}
	}
}

// What it costs when this fails: uid 1001 mounts FUSE on its own folder with allow_other and a
// source, subtype or mount point holding a byte Go counts as space and the kernel does not escape,
// \r, \v, \f, U+0085 or U+00A0, that spells user_id=1000, and trust reads uid 1000 as the mounter, so
// uid 1000's ReadOwned and Path pass a name on a server uid 1001 runs; or a mount point spells a
// virtiofs tail, which has no mounter, or a disk's type for mountKind.
func TestAMountinfoLineIsSplitOnTheKernelsSpaceAlone(t *testing.T) {
	t.Parallel()
	const opts = " rw,user_id=1001,group_id=1001,allow_other\n"
	line := func(point, tail string) string {
		return "22 1 259:2 / / rw,relatime shared:1 - ext4 /dev/nvme0n1p2 rw\n" +
			"41 22 0:52 / " + point + " rw,nosuid,nodev,relatime shared:30 - " + tail + opts
	}
	cases := []struct{ name, info, on string }{
		{"the plain line", line("/home/b", "fuse x"), "/home/b"},
		{"a \\r in the source", line("/home/b", "fuse x\rrw,user_id=1000"), "/home/b"},
		{"a U+00A0 in the source", line("/home/b", "fuse x rw,user_id=1000"), "/home/b"},
		{"a \\r in a fuse. subtype", line("/home/b", "fuse.x\ry\rrw,user_id=1000 src"), "/home/b"},
		{"a mount point spelling a virtiofs tail", line("/home/b/m\rx\ry\r-\rvirtiofs\rs\rrw", "fuse x"), "/home/b/m\rx\ry\r-\rvirtiofs\rs\rrw"},
		{"a \\v in the source", line("/home/b", "fuse x\vrw,user_id=1000"), "/home/b"},
		{"a \\f in the source", line("/home/b", "fuse x\frw,user_id=1000"), "/home/b"},
		{"a U+0085 in the source", line("/home/b", "fuse x\u0085rw,user_id=1000"), "/home/b"},
	}
	for _, c := range cases {
		if who, on := fuseMounter(c.info, "0:52"); who != "uid:1001" || on != c.on {
			t.Errorf("%s: answered %q on %q, and want %q on %q", c.name, who, on, "uid:1001", c.on)
		}
	}
	h := disk(nil).host("uid:1000")
	n, err := mounterOf(file("uid:1000", 0o600), "/home/b/f", "0:52", []byte(line("/home/b", "fuse x\rrw,user_id=1000")), nil)
	if err != nil {
		t.Fatal(err)
	}
	wantRefused(t, "ReadOwned, of uid 1000's file on uid 1001's mount, its source spelling user_id=1000", judgeRead(n, "/home/b/f", "uid:1000", true, 10, h),
		refusal{ErrNotOwners, Fix{}, " by uid:1001, which is not trusted"})
	if got := mountKind(line("/mnt/x\ry\r-\rbtrfs\rs\rrw", "fuse x"), "41"); got != "fuse" {
		t.Errorf("a mount point spelling a btrfs tail: of type %q, and want %q", got, "fuse")
	}
}

// What it costs when this fails: a reader in a user namespace of its own takes a host account's
// allow_other mount, whose user_id= is the reader's own number there or root's, as its own or
// root's, and it passes; or host root that joined a rootless container's mount namespace reads its
// fuse-overlayfs's mounter as root's.
func TestAFUSEMounterIsReadOnlyInTheInitialUserNamespace(t *testing.T) {
	t.Parallel()
	initial, child := nsFile{4, initUserNS}, nsFile{4, 0xF0000123}
	cases := []struct {
		name       string
		who        string
		own, mount nsFile
		want       string
	}{
		{"both initial", "uid:0", initial, initial, "uid:0"},
		{"host root in a container's mount namespace", "uid:0", initial, child, unnamed},
		{"a reader in a container", "uid:1000", child, child, unnamed},
		{"the mount namespace's owner not read", "uid:0", initial, nsFile{}, unnamed},
		{"this namespace not read", "uid:0", nsFile{}, initial, unnamed},
		{"the initial inode on another device", "uid:0", nsFile{9, initUserNS}, initial, unnamed},
		{"virtiofs, which has no mounter", "", child, child, ""},
	}
	for _, c := range cases {
		if got := ofInitialNS(c.who, c.own, c.mount); got != c.want {
			t.Errorf("%s: answered %q, and want %q", c.name, got, c.want)
		}
	}
}

// What it costs when this fails: an owner of this mount namespace who sees the host's processes
// mounts over /proc and serves a mountinfo whose line for its FUSE mount is virtiofs, which has no
// mounter, or names root as user_id=, and the name passes.
func TestAMountinfoNotOnProcfsNamesNoMounter(t *testing.T) {
	t.Parallel()
	const info = "41 22 0:52 / /home/a rw - fuse.sshfs a@b:/ rw,user_id=0,group_id=0,allow_other\n"
	const f = "/home/a/f"
	other := errors.New("a read that failed")
	cases := []struct {
		name    string
		b       []byte
		err     error
		who, on string
		fails   error
		refusal refusal
	}{
		{"read from procfs", []byte(info), nil, "uid:0", "/home/a", nil, refusal{}},
		{"not on procfs", []byte(info), &fs.PathError{Op: "fstatfs", Path: "/proc/self/mountinfo", Err: errNotProc}, unnamed, f, nil,
			refusal{ErrNotOwners, Fix{}, unnamed}},
		{"not read", nil, other, "", "", other, refusal{}},
	}
	h := disk(nil).host(meID)
	for _, c := range cases {
		n, err := mounterOf(file(rootID, 0o644), f, "0:52", c.b, c.err)
		if !errors.Is(err, c.fails) || n.mounter != c.who || n.mountedOn != c.on {
			t.Errorf("%s: mounted by %q on %q, %v, and want %q on %q, %v", c.name, n.mounter, n.mountedOn, err, c.who, c.on, c.fails)
		}
		if err == nil {
			wantRefused(t, c.name, owned(n, place{path: f, last: true}, Who{}, h), c.refusal)
		}
	}
}

// What it costs when this fails: on Linux a name on CIFS, 9p, vfat or squashfs, whose server or
// image reports any owner, or on an account's stick or image that udisks or fusermount mounted
// nosuid, passes on the owners it reports; or root's ext4, a tmpfs, or what the app names is
// refused.
func TestANameOnAMountWhoseOwnersAreNotJudgedIsRefused(t *testing.T) {
	t.Parallel()
	const cifs, ext4, tmpfs = 0xFF534D42, 0xEF53, 0x01021994
	d, f := at("d"), at("d", "f")
	on := func(n node, typ uint32, nosuid bool) node { n.fsType, n.nosuid, n.id.vol = typ, nosuid, 7; return n }
	typed := " is on a file system of type 0xff534d42, whose owners trust does not judge"
	nosuid := " is on a file system a disk backs, mounted nosuid as an account's stick or image is, and on no mount named as trusted"
	cases := []struct {
		name   string
		typ    uint32
		nosuid bool
		w      Who
		r      refusal
	}{
		{"a type outside the list", cifs, false, Who{}, refusal{ErrMount, Fix{}, d + typed}},
		{"the same type, which the app names", cifs, false, Who{FileSystems: []uint32{cifs}}, refusal{}},
		{"a type the app names, mounted nosuid", cifs, true, Who{FileSystems: []uint32{cifs}}, refusal{ErrMount, Fix{}, d + nosuid}},
		{"a disk mounted nosuid", ext4, true, Who{}, refusal{ErrMount, Fix{}, d + nosuid}},
		{"the same, its folder of Who.Mounts", ext4, true, Who{Mounts: []string{d}}, refusal{}},
		{"the same, Who.Mounts naming another device", ext4, true, Who{Mounts: []string{top()}}, refusal{ErrMount, Fix{}, d + nosuid}},
		{"the same, Who.Mounts not absolute", ext4, true, Who{Mounts: []string{"d"}}, refusal{ErrMount, Fix{}, d + nosuid}},
		{"the same, Who.Mounts not read", ext4, true, Who{Mounts: []string{at("gone")}}, refusal{ErrMount, Fix{}, d + nosuid}},
		{"a disk mounted with setuid", ext4, false, Who{}, refusal{}},
		{"ZFS", zfsMagic, false, Who{}, refusal{}},
		{"ZFS mounted nosuid", zfsMagic, true, Who{}, refusal{ErrMount, Fix{}, d + nosuid}},
		{"a tmpfs mounted nosuid", tmpfs, true, Who{}, refusal{}},
		{"FUSE, by its own rule", 0x65735546, true, Who{}, refusal{}},
		{"no type read, as off Linux", 0, true, Who{}, refusal{}},
	}
	walked := func(name string, nodes map[string]node, w Who, r refusal) {
		dk := disk(nodes)
		h := dk.host(meID)
		h.ident = func(p string) (fileID, error) {
			if p == "d" { // as os.Stat reads it, from a working folder holding d: only the rule's own guard refuses it
				p = d
			}
			n, ok := dk.nodes[p]
			if !ok {
				return fileID{}, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
			}
			return n.id, nil
		}
		_, err := walk(f, File, w, h)
		wantRefused(t, name, err, r)
		if r != (refusal{}) && !errors.Is(err, ErrNotOwners) {
			t.Errorf("%s: %v does not match ErrNotOwners", name, err)
		}
	}
	// On Linux fstatfs reads the type, typed, which is held to its mount, as a 9p server answers
	// fstatfs with its host's type: ext2/3/4,
	// xfs and f2fs by a device whose major is not 0, anon, every other by kind, mountinfo's,
	// and a 9p mount as 9p; and a FUSE mount root made nosuid, as udisks makes an account's stick,
	// is asked the nosuid half too.
	untyped := " is on a file system of type 0, as fstatfs answers it, which a 9p server may"
	disagrees := func(typ uint32, kind string) string {
		return fmt.Sprintf(" is on a file system of type %#x by fstatfs, which a 9p server answers as it likes, where mountinfo gives its mount the type %q", typ, kind)
	}
	ninep := func(typ uint32) string {
		return fmt.Sprintf(" is on a file system of type %#x by fstatfs, on a mount mountinfo gives the type \"9p\", whose server answers fstatfs as it likes, and no file system type named as trusted is 9p's, 0x1021997", typ)
	}
	anon := func(typ uint32) string {
		return fmt.Sprintf(" is on a file system of type %#x by fstatfs, a disk's, on a device of major 0, as a 9p mount's is and no disk's", typ)
	}
	unread := func(typ uint32) string {
		return fmt.Sprintf(" is on a file system of type %#x by fstatfs, where its mount's type is not read from mountinfo, as before Linux 5.8", typ)
	}
	const xfs, btrfs, overlay, nfs, ninePs = 0x58465342, 0x9123683E, 0x794C7630, 0x6969, 0x01021997
	nineNamed := Who{FileSystems: []uint32{ninePs}}
	rootFUSE := " is on a file system FUSE's, mounted nosuid by root, as udisks mounts an account's stick with ntfs-3g or exfat-fuse, and on no mount named as trusted"
	for _, c := range []struct {
		name, kind, mounter string
		typ                 uint32
		nosuid, anon        bool
		w                   Who
		r                   refusal
	}{
		{"ext4 on a disk's device, its mount not read", "", "", ext4, false, false, Who{}, refusal{}},
		{"ext4 on a device of major 0", "", "", ext4, false, true, Who{}, refusal{ErrMount, Fix{}, d + anon(ext4)}},
		{"xfs on a device of major 0", "", "", xfs, false, true, Who{}, refusal{ErrMount, Fix{}, d + anon(xfs)}},
		{"ext4 on a device of major 0, its mount not read, where the app names 9p", "", "", ext4, false, true, nineNamed, refusal{ErrMount, Fix{}, d + anon(ext4)}},
		{"ext4 on a device of major 0, its mount 9p's", "9p", "", ext4, false, true, Who{}, refusal{ErrMount, Fix{}, d + ninep(ext4)}},
		{"ext4 on a device of major 0, its mount 9p's, where the app names 9p", "9p", "", ext4, false, true, nineNamed, refusal{}},
		{"xfs on a device of major 0, its mount 9p's, where the app names 9p", "9p", "", xfs, false, true, nineNamed, refusal{}},
		{"ext4 on a device of major 0, its mount tmpfs's, where the app names 9p", "tmpfs", "", ext4, false, true, nineNamed, refusal{ErrMount, Fix{}, d + anon(ext4)}},
		{"ext4, its mount 9p's", "9p", "", ext4, false, false, Who{}, refusal{ErrMount, Fix{}, d + ninep(ext4)}},
		{"ZFS, its mount 9p's", "9p", "", zfsMagic, false, false, Who{}, refusal{ErrMount, Fix{}, d + ninep(zfsMagic)}},
		{"tmpfs, its mount 9p's", "9p", "", tmpfs, false, false, Who{}, refusal{ErrMount, Fix{}, d + ninep(tmpfs)}},
		{"overlayfs, its mount 9p's", "9p", "", overlay, false, false, Who{}, refusal{ErrMount, Fix{}, d + ninep(overlay)}},
		{"NFS, its mount 9p's", "9p", "", nfs, false, false, Who{}, refusal{ErrMount, Fix{}, d + ninep(nfs)}},
		{"FUSE, its mount 9p's", "9p", meID, fuseMagic, false, false, Who{}, refusal{ErrMount, Fix{}, d + ninep(fuseMagic)}},
		{"tmpfs, its mount 9p's, where the app names 9p", "9p", "", tmpfs, false, false, nineNamed, refusal{}},
		{"the same, mounted nosuid", "9p", "", tmpfs, true, false, nineNamed, refusal{ErrMount, Fix{}, d + nosuid}},
		{"tmpfs, its mount's", "tmpfs", "", tmpfs, false, false, Who{}, refusal{}},
		{"tmpfs, its mount not read", "", "", tmpfs, false, false, Who{}, refusal{ErrMount, Fix{}, d + unread(tmpfs)}},
		{"btrfs, its mount's", "btrfs", "", btrfs, false, false, Who{}, refusal{}},
		{"btrfs, its mount ext4's", "ext4", "", btrfs, false, false, Who{}, refusal{ErrMount, Fix{}, d + disagrees(btrfs, "ext4")}},
		{"btrfs, its mount not read", "", "", btrfs, false, false, Who{}, refusal{ErrMount, Fix{}, d + unread(btrfs)}},
		{"NFS, its mount not read", "", "", nfs, false, false, Who{}, refusal{ErrMount, Fix{}, d + unread(nfs)}},
		{"ZFS, its mount not read", "", "", zfsMagic, false, false, Who{}, refusal{ErrMount, Fix{}, d + unread(zfsMagic)}},
		{"a type the app names, its mount 9p's", "9p", "", cifs, false, false, Who{FileSystems: []uint32{cifs}}, refusal{ErrMount, Fix{}, d + ninep(cifs)}},
		{"a type the app names, its mount's", "cifs", "", cifs, false, false, Who{FileSystems: []uint32{cifs}}, refusal{}},
		{"a type the app names, its mount not read", "", "", cifs, false, false, Who{FileSystems: []uint32{cifs}}, refusal{ErrMount, Fix{}, d + unread(cifs)}},
		{"9p's own type, which the app names", "9p", "", ninePs, false, false, nineNamed, refusal{}},
		{"0 from fstatfs", "9p", "", 0, false, false, Who{}, refusal{ErrMount, Fix{}, d + untyped}},
		{"0 from fstatfs, its mount ext4's", "ext4", "", 0, false, false, Who{}, refusal{ErrMount, Fix{}, d + untyped}},
		{"FUSE root mounted nosuid", "fuseblk", rootID, fuseMagic, true, false, Who{}, refusal{ErrMount, Fix{}, d + rootFUSE}},
		{"the same, its folder of Who.Mounts", "fuseblk", rootID, fuseMagic, true, false, Who{Mounts: []string{d}}, refusal{}},
		{"FUSE root mounted with setuid", "fuseblk", rootID, fuseMagic, false, false, Who{}, refusal{}},
		{"FUSE this account mounted nosuid, by the mounter rule", "fuse.sshfs", meID, fuseMagic, true, false, Who{}, refusal{}},
		{"FUSE, its mount not read", "", meID, fuseMagic, false, false, Who{}, refusal{ErrMount, Fix{}, d + unread(fuseMagic)}},
	} {
		typed := func(n node) node {
			n = on(n, c.typ, c.nosuid)
			n.typed, n.kind, n.mounter, n.anon = true, c.kind, c.mounter, c.anon
			return n
		}
		walked(c.name, map[string]node{d: typed(dir(rootID, 0o755)), f: typed(file(rootID, 0o644))}, c.w, c.r)
	}
	for _, c := range cases {
		walked(c.name, map[string]node{d: on(dir(rootID, 0o755), c.typ, c.nosuid), f: on(file(rootID, 0o644), c.typ, c.nosuid)}, c.w, c.r)
	}

	// A mount noexec without nosuid, as usbmount mounts an account's stick, is asked as one nosuid
	// is: no setuid program the volume holds runs there. A FUSE row is typed, its
	// mount read, as the walk reads one.
	noexec := " is on a file system a disk backs, mounted noexec without nosuid as usbmount mounts an account's stick, and on no mount named as trusted"
	noexecFUSE := " is on a file system FUSE's, mounted noexec without nosuid by root, as an automounter may mount an account's stick with ntfs-3g, and on no mount named as trusted"
	for _, c := range []struct {
		name, kind, mounter string
		typ                 uint32
		nosuid              bool
		w                   Who
		r                   refusal
	}{
		{"a disk mounted noexec without nosuid", "", "", ext4, false, Who{}, refusal{ErrMount, Fix{}, d + noexec}},
		{"the same, its folder of Who.Mounts", "", "", ext4, false, Who{Mounts: []string{d}}, refusal{}},
		{"the same, Who.Mounts naming another device", "", "", ext4, false, Who{Mounts: []string{top()}}, refusal{ErrMount, Fix{}, d + noexec}},
		{"the same, Who.Mounts not absolute", "", "", ext4, false, Who{Mounts: []string{"d"}}, refusal{ErrMount, Fix{}, d + noexec}},
		{"a disk mounted noexec and nosuid", "", "", ext4, true, Who{}, refusal{ErrMount, Fix{}, d + nosuid}},
		{"ZFS mounted noexec without nosuid", "", "", zfsMagic, false, Who{}, refusal{ErrMount, Fix{}, d + noexec}},
		{"a type the app names, mounted noexec without nosuid", "", "", cifs, false, Who{FileSystems: []uint32{cifs}}, refusal{ErrMount, Fix{}, d + noexec}},
		{"a tmpfs mounted noexec without nosuid", "", "", tmpfs, false, Who{}, refusal{}},
		{"FUSE root mounted noexec without nosuid", "fuseblk", rootID, fuseMagic, false, Who{}, refusal{ErrMount, Fix{}, d + noexecFUSE}},
		{"the same, its folder of Who.Mounts", "fuseblk", rootID, fuseMagic, false, Who{Mounts: []string{d}}, refusal{}},
		{"FUSE this account mounted noexec without nosuid, by the mounter rule", "fuse.sshfs", meID, fuseMagic, false, Who{}, refusal{}},
	} {
		marked := func(n node) node {
			n = on(n, c.typ, c.nosuid)
			n.noexec, n.typed, n.kind, n.mounter = true, c.kind != "", c.kind, c.mounter
			return n
		}
		walked(c.name, map[string]node{d: marked(dir(rootID, 0o755)), f: marked(file(rootID, 0o644))}, c.w, c.r)
	}

	h := disk(nil).host(meID)
	key := file(meID, 0o600)
	h.ident = func(string) (fileID, error) { return fileID{vol: 7}, nil }
	for _, c := range []struct {
		name   string
		typ    uint32
		nosuid bool
		w      Who
		r      refusal
	}{
		{"a type outside the list", cifs, false, Who{}, refusal{ErrMount, Fix{}, f + typed}},
		{"the same type, which the app names", cifs, false, Who{FileSystems: []uint32{cifs}}, refusal{}},
		{"a disk mounted nosuid", ext4, true, Who{}, refusal{ErrMount, Fix{}, f + nosuid}},
		{"the same, its folder of Who.Mounts", ext4, true, Who{Mounts: []string{d}}, refusal{}},
	} {
		h.mounts = c.w
		wantRefused(t, "ReadOwned, "+c.name, judgeRead(on(key, c.typ, c.nosuid), f, meID, true, 10, h), c.r)
	}

	noexecKey := on(key, ext4, false)
	noexecKey.noexec = true
	h.mounts = Who{}
	wantRefused(t, "ReadOwned, a disk mounted noexec without nosuid", judgeRead(noexecKey, f, meID, true, 10, h), refusal{ErrMount, Fix{}, f + noexec})
	h.mounts = Who{Mounts: []string{d}}
	wantRefused(t, "ReadOwned, the same, its folder of Who.Mounts", judgeRead(noexecKey, f, meID, true, 10, h), refusal{})

	h.mounts = Who{}
	nine := on(key, ext4, false)
	nine.typed, nine.kind = true, "9p"
	wantRefused(t, "ReadOwned, ext4, its mount 9p's", judgeRead(nine, f, meID, true, 10, h), refusal{ErrMount, Fix{}, f + ninep(ext4)})

	// A ZFS name carrying TrueNAS SCALE's list is refused, even where the app names ZFS.
	listed := " is on a file system of type 0x2fc12fc1, ZFS, where it carries an NFSv4 list, system.nfs4_acl_xdr, that trust does not read"
	zl := func(n node) node { n = on(n, zfsMagic, false); n.zfsList = true; return n }
	zfs := Who{FileSystems: []uint32{zfsMagic}}
	_, err := walk(f, File, zfs, disk(map[string]node{d: on(dir(rootID, 0o755), zfsMagic, false), f: zl(file(rootID, 0o644))}).host(meID))
	wantRefused(t, "ZFS, its list kept", err, refusal{ErrMount, Fix{}, f + listed})
	h.mounts = zfs
	wantRefused(t, "ReadOwned, ZFS, its list kept", judgeRead(zl(key), f, meID, true, 10, h), refusal{ErrMount, Fix{}, f + listed})
}

package appdirs_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/birdtable/go-files/appdirs"
)

// Volume's rule on macOS, held on every system: APFS, HFS+, FAT and exFAT with the local-mount
// flag and no File Provider mark are Local; a type that holds fuse is FUSE whatever the flag says;
// NFS, SMB, AFP, WebDAV, CIFS and FTP are Network, FUSE-T's NFS and SMB among them; a mark is
// SyncPlaceholder; and a mark that cannot be told, no local-mount flag and any other type are
// Unknown. Type is the type given.
//
// What it costs when this fails: a macFUSE volume mounted with -o local, an iCloud Drive folder or
// an SMB share is answered Local, and a program keeps its database where its locks do not hold.
func TestDarwinVolumeRule(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		typ   string
		local bool
		sync  appdirs.SyncAnswer
		want  appdirs.VolumeKind
	}{
		{"apfs", true, appdirs.SyncNone, appdirs.Local},
		{"hfs", true, appdirs.SyncNone, appdirs.Local},
		{"msdos", true, appdirs.SyncNone, appdirs.Local},
		{"exfat", true, appdirs.SyncNone, appdirs.Local},
		{"apfs", true, appdirs.SyncMarked, appdirs.SyncPlaceholder},
		{"apfs", true, appdirs.SyncCannotTell, appdirs.Unknown},
		{"apfs", false, appdirs.SyncNone, appdirs.Unknown},
		{"macfuse", true, appdirs.SyncNone, appdirs.FUSE},
		{"osxfuse", false, appdirs.SyncNone, appdirs.FUSE},
		{"macfuse", true, appdirs.SyncMarked, appdirs.FUSE},
		{"nfs", false, appdirs.SyncNone, appdirs.Network},
		{"nfs", true, appdirs.SyncNone, appdirs.Network},
		{"smbfs", false, appdirs.SyncNone, appdirs.Network},
		{"afpfs", false, appdirs.SyncNone, appdirs.Network},
		{"webdav", false, appdirs.SyncNone, appdirs.Network},
		{"cifs", false, appdirs.SyncNone, appdirs.Network},
		{"ftp", false, appdirs.SyncNone, appdirs.Network},
		{"devfs", true, appdirs.SyncNone, appdirs.Unknown},
		{"autofs", false, appdirs.SyncNone, appdirs.Unknown},
		{"lifs", true, appdirs.SyncNone, appdirs.Unknown},
		{"", true, appdirs.SyncNone, appdirs.Unknown},
	} {
		got := appdirs.DarwinVolume(c.typ, c.local, c.sync)
		if got.Kind != c.want || got.Type != c.typ || got.Local() != (c.want == appdirs.Local) {
			t.Errorf("%q, local flag %v, sync %d: %+v, Local %v, want %v", c.typ, c.local, c.sync, got, got.Local(), c.want)
		}
	}
}

// What the File Provider's marks say of a path on macOS, held on every system: a file the provider
// evicted, or an attribute the domain's mark or com.apple.fileprovider. leads, on the path or any
// folder above it, is marked, before any other answer; a folder whose attributes cannot be listed,
// or a CloudStorage or Mobile Documents folder in a Library folder, in any case, with no mark,
// cannot be told; any other path is none, its every folder up to / asked.
//
// What it costs when this fails: a folder in iCloud Drive or a third-party File Provider domain,
// whose files the provider may evict, is answered as a plain APFS folder, so Local.
func TestDarwinSyncRule(t *testing.T) {
	t.Parallel()
	denied := errors.New("listxattr denied")
	attrs := map[string][]string{
		"/Users/a/Library/Mobile Documents":                       {"com.apple.file-provider-domain-id"},
		"/Users/a/Library/Mobile Documents/com~apple~CloudDocs/b": {"com.apple.FinderInfo"},
		"/Users/a/Booking":     {"com.apple.fileprovider.dir#N"},
		"/Users/a/Domain":      {"com.apple.file-provider-domain-id#PN"},
		"/Users/a/Documents":   {"com.apple.FinderInfo", "com.apple.metadata:kMDItemWhereFroms"},
		"/Users/a/Downloads/f": {"com.apple.fileprovider.fpfs#P"},
	}
	fails := map[string]bool{"/Users/b": true, "/Users/c/Library/Mobile Documents": true}
	for _, c := range []struct {
		path     string
		dataless bool
		want     appdirs.SyncAnswer
	}{
		{"/Users/a/Library/Mobile Documents", false, appdirs.SyncMarked},
		{"/Users/a/Library/Mobile Documents/com~apple~CloudDocs/b/data", false, appdirs.SyncMarked},
		{"/Users/a/Booking/data", false, appdirs.SyncMarked},
		{"/Users/a/Domain/data", false, appdirs.SyncMarked},
		{"/Users/a/Downloads/f", false, appdirs.SyncMarked},
		{"/Users/a/Library/Application Support/app", true, appdirs.SyncMarked},
		{"/Users/a/Library/CloudStorage/Dropbox/data", false, appdirs.SyncCannotTell},
		{"/Users/a/Library/CloudStorage", false, appdirs.SyncCannotTell},
		{"/Users/a/library/CloudStorage/Dropbox/data", false, appdirs.SyncCannotTell},
		{"/Users/a/library/cloudstorage/Dropbox/data", false, appdirs.SyncCannotTell},
		{"/Users/a/library/Mobile Documents/x/data", false, appdirs.SyncCannotTell},
		{"/Users/a/LIBRARY/mobile documents/x/data", false, appdirs.SyncCannotTell},
		{"/Library/Mobile Documents/x", false, appdirs.SyncCannotTell},
		{"/Users/b/data", false, appdirs.SyncCannotTell},
		{"/Users/c/Library/Mobile Documents/x", false, appdirs.SyncCannotTell},
		{"/Users/a/Library/Application Support/app", false, appdirs.SyncNone},
		{"/Users/a/Documents/data", false, appdirs.SyncNone},
		{"/Users/a/CloudStorage/data", false, appdirs.SyncNone},
		{"/Users/a/Library/Caches/CloudStorage/data", false, appdirs.SyncNone},
		{"/", false, appdirs.SyncNone},
	} {
		var asked []string
		names := func(p string) ([]string, error) {
			asked = append(asked, p)
			if fails[p] {
				return nil, denied
			}
			return attrs[p], nil
		}
		got := appdirs.DarwinSync(c.path, c.dataless, names)
		if got != c.want {
			t.Errorf("%s, dataless %v: %d, want %d, its folders asked %q", c.path, c.dataless, got, c.want, asked)
		}
		if got == appdirs.SyncNone && (len(asked) == 0 || asked[len(asked)-1] != "/") {
			t.Errorf("%s: none, with its folders asked %q, not each of them up to /", c.path, asked)
		}
	}
}

// Volume's rule on Linux, held on every system: the magic number alone decides. ext2 to ext4, XFS,
// btrfs, f2fs, tmpfs, ZFS, FAT, exFAT, bcachefs and overlay are Local; NFS, SMB, CIFS, SMB2, AFS,
// kAFS, Ceph, Coda and 9p are Network; FUSE is FUSE, fuseblk among them; any other, procfs and
// sysfs among them, is Unknown. Type is the mount table's fstype, or the number in hex.
//
// What it costs when this fails: an sshfs or NFS mount is answered Local, or one whose fstype
// reads like a local one decides the answer.
func TestLinuxVolumeRule(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		magic    uint32
		fstype   string
		want     appdirs.VolumeKind
		wantType string
	}{
		{0xef53, "ext4", appdirs.Local, "ext4"},
		{0xef53, "ext3", appdirs.Local, "ext3"},
		{0x58465342, "xfs", appdirs.Local, "xfs"},
		{0x9123683e, "btrfs", appdirs.Local, "btrfs"},
		{0xf2f52010, "f2fs", appdirs.Local, "f2fs"},
		{0x01021994, "tmpfs", appdirs.Local, "tmpfs"},
		{0x2fc12fc1, "zfs", appdirs.Local, "zfs"},
		{0x4d44, "vfat", appdirs.Local, "vfat"},
		{0x2011bab0, "exfat", appdirs.Local, "exfat"},
		{0xca451a4e, "bcachefs", appdirs.Local, "bcachefs"},
		{0x794c7630, "overlay", appdirs.Local, "overlay"},
		{0x6969, "nfs4", appdirs.Network, "nfs4"},
		{0x517b, "smbfs", appdirs.Network, "smbfs"},
		{0xff534d42, "cifs", appdirs.Network, "cifs"},
		{0xfe534d42, "smb3", appdirs.Network, "smb3"},
		{0x5346414f, "afs", appdirs.Network, "afs"},
		{0x6b414653, "afs", appdirs.Network, "afs"},
		{0x00c36400, "ceph", appdirs.Network, "ceph"},
		{0x73757245, "coda", appdirs.Network, "coda"},
		{0x01021997, "9p", appdirs.Network, "9p"},
		{0x65735546, "fuse.sshfs", appdirs.FUSE, "fuse.sshfs"},
		{0x65735546, "fuse.rclone", appdirs.FUSE, "fuse.rclone"},
		{0x65735546, "fuseblk", appdirs.FUSE, "fuseblk"},
		{0x65735546, "ext4", appdirs.FUSE, "ext4"},
		{0x6969, "ext4", appdirs.Network, "ext4"},
		{0x9fa0, "proc", appdirs.Unknown, "proc"},
		{0x62656572, "sysfs", appdirs.Unknown, "sysfs"},
		{0x5346544e, "ntfs", appdirs.Unknown, "ntfs"},
		{0xef53, "", appdirs.Local, "0xef53"},
		{0x6969, "", appdirs.Network, "0x6969"},
	} {
		got := appdirs.LinuxVolume(c.magic, c.fstype)
		if got.Kind != c.want || got.Type != c.wantType {
			t.Errorf("magic %#x, fstype %q: %+v, want %v and Type %q", c.magic, c.fstype, got, c.want, c.wantType)
		}
	}
}

// The mount table's fstype for a path, held on every system: the line of the longest mount point
// that is the path or a folder above it, the later of two lines with one mount point, a mount point
// read back from mountinfo's octal escapes, a mount point that only begins like a folder of the
// path passed over, and a line too short or with no separator skipped.
//
// What it costs when this fails: an sshfs mount over a folder of the home is named by the home's
// ext4 line, or a folder named /homes is taken for /home's mount.
func TestMountTypeIsTheLongestMountPointThatHoldsThePath(t *testing.T) {
	t.Parallel()
	const table = `22 1 8:1 / / rw,relatime shared:1 - ext4 /dev/sda1 rw
23 22 0:21 / /proc rw,nosuid shared:12 - proc proc rw
30 22 8:2 / /home rw,relatime shared:2 - xfs /dev/sda2 rw
31 30 0:45 / /home/a/remote\040box rw,nosuid,nodev shared:3 - fuse.sshfs host:/ rw,user_id=1000
32 22 0:46 / /homes rw shared:4 - nfs4 srv:/homes rw
33 22 0:47 / /mnt rw shared:5 - ext4 /dev/sdb1 rw
34 33 0:48 / /mnt rw shared:6 - cifs //srv/share rw
35 22 0:49 / /short rw
36 22 0:50 / /noseparator rw shared:7 tmpfs tmpfs rw
37 22 0:51 / /tail rw shared:8 -
`
	for _, c := range []struct{ path, want string }{
		{"/", "ext4"},
		{"/etc/hosts", "ext4"},
		{"/proc/self", "proc"},
		{"/home", "xfs"},
		{"/home/a/data", "xfs"},
		{"/home/a/remote box", "fuse.sshfs"},
		{"/home/a/remote box/data", "fuse.sshfs"},
		{"/home/a/remote boxes", "xfs"},
		{"/homes/b", "nfs4"},
		{"/homesick", "ext4"},
		{"/mnt/x", "cifs"},
		{"/short/x", "ext4"},
		{"/noseparator/x", "ext4"},
		{"/tail/x", "ext4"},
	} {
		if got := appdirs.MountType(table, c.path); got != c.want {
			t.Errorf("MountType(%s) = %q, want %q", c.path, got, c.want)
		}
	}
	if got := appdirs.MountType("", "/x"); got != "" {
		t.Errorf("MountType of an empty table = %q, want none", got)
	}
}

// Volume's rule on Windows, held on every system: a remote drive or a path through \Device\Mup is
// Network, a file system whose name holds FUSE is FUSE, a path under a sync root is
// SyncPlaceholder, and only NTFS, ReFS, FAT, FAT32 and exFAT on a numbered local disk volume, fixed
// or removable, under no sync root, are Local. A sync root that cannot be told, another device,
// another drive type and another file system are Unknown. Type is the file system's name.
//
// What it costs when this fails: a mapped share, a WinFsp or Dokan drive that names itself NTFS, or
// a OneDrive folder is answered Local.
func TestWindowsVolumeRule(t *testing.T) {
	t.Parallel()
	const disk, mup, dokan = `\Device\HarddiskVolume3\Users\a\data`, `\Device\Mup\srv\share\data`, `\Device\Volume{d6cc17c5-1734-4085-bce7-964f1e9f5de9}\data`
	for _, c := range []struct {
		nt, fsName string
		drive      uint32
		sync       appdirs.SyncAnswer
		want       appdirs.VolumeKind
	}{
		{disk, "NTFS", 3, appdirs.SyncNone, appdirs.Local},
		{disk, "ReFS", 3, appdirs.SyncNone, appdirs.Local},
		{disk, "FAT32", 2, appdirs.SyncNone, appdirs.Local},
		{disk, "FAT", 2, appdirs.SyncNone, appdirs.Local},
		{disk, "exFAT", 2, appdirs.SyncNone, appdirs.Local},
		{`\Device\HarddiskVolume12\`, "NTFS", 3, appdirs.SyncNone, appdirs.Local},
		{disk, "NTFS", 4, appdirs.SyncNone, appdirs.Network},
		{mup, "NTFS", 4, appdirs.SyncNone, appdirs.Network},
		{mup, "NTFS", 3, appdirs.SyncNone, appdirs.Network},
		{mup, "NTFS", 0, appdirs.SyncCannotTell, appdirs.Network},
		{dokan, "FUSE-sshfs", 3, appdirs.SyncNone, appdirs.FUSE},
		{dokan, "WinFsp-FUSE", 3, appdirs.SyncNone, appdirs.FUSE},
		{disk, "NTFS", 3, appdirs.SyncMarked, appdirs.SyncPlaceholder},
		{disk, "NTFS", 3, appdirs.SyncCannotTell, appdirs.Unknown},
		{dokan, "NTFS", 3, appdirs.SyncNone, appdirs.Unknown},
		{`\Device\CdRom0\x`, "CDFS", 5, appdirs.SyncNone, appdirs.Unknown},
		{disk, "NTFS", 6, appdirs.SyncNone, appdirs.Unknown},
		{disk, "NTFS", 0, appdirs.SyncNone, appdirs.Unknown},
		{disk, "UDF", 3, appdirs.SyncNone, appdirs.Unknown},
		{`\Device\HarddiskVolume\x`, "NTFS", 3, appdirs.SyncNone, appdirs.Unknown},
		{`\Device\HarddiskVolume3x\x`, "NTFS", 3, appdirs.SyncNone, appdirs.Unknown},
		{`\Device\HarddiskVolume3`, "NTFS", 3, appdirs.SyncNone, appdirs.Unknown},
	} {
		got := appdirs.WindowsVolume(c.nt, c.fsName, c.drive, c.sync)
		if got.Kind != c.want || got.Type != c.fsName {
			t.Errorf("%s, %s, drive type %d, sync %d: %+v, want %v", c.nt, c.fsName, c.drive, c.sync, got, c.want)
		}
	}
}

// What the Cloud Files marks say of a path on Windows, held on every system: a Cloud Files reparse
// tag, IO_REPARSE_TAG_CLOUD or a variant, on the path or any folder above it up to the volume's
// root, or a registered sync root that is the path or a folder above it, matched by whole names
// with no case and with or without a \\?\ prefix or a separator at its end, is marked, before any
// other answer; a tag that cannot be read, or roots that could not be read, with no mark, cannot be
// told; any other path is none, its every folder up to the root asked, a junction's tag and a root
// that only begins like a folder of the path or lies below it among them.
//
// What it costs when this fails: a folder in OneDrive, or below another provider's sync root, is
// answered as a plain NTFS folder, so Local.
func TestWindowsSyncRule(t *testing.T) {
	t.Parallel()
	denied := errors.New("access denied")
	tags := map[string]uint32{
		`C:\Users\a\OneDrive`:          0x9000601a,
		`C:\Users\a\Box\f`:             0x9000001a,
		`C:\Users\a\Junction`:          0xa0000003,
		`C:\Users\a\Dedup\f`:           0x80000013,
		`C:\Users\a\Cloud7`:            0x9000701a,
		`C:\Users\a\NotCloud`:          0x9000101b,
		`C:\Users\a\AppData\Local\app`: 0,
	}
	fails := map[string]bool{`C:\Users\b`: true}
	roots := []string{`C:\Users\a\Sync Root\`, `\\?\D:\Provider`, `C:\Users\a\Deep\Below\Root`, `e:\`}
	for _, c := range []struct {
		path     string
		roots    []string
		rootsErr error
		want     appdirs.SyncAnswer
	}{
		{`C:\Users\a\OneDrive`, nil, nil, appdirs.SyncMarked},
		{`C:\Users\a\OneDrive\Documents\data`, nil, nil, appdirs.SyncMarked},
		{`C:\Users\a\Box\f`, nil, nil, appdirs.SyncMarked},
		{`C:\Users\a\Cloud7\x`, nil, nil, appdirs.SyncMarked},
		{`C:\Users\a\OneDrive\x`, nil, denied, appdirs.SyncMarked},
		{`C:\Users\a\sync root\data`, roots, nil, appdirs.SyncMarked},
		{`C:\USERS\A\SYNC ROOT`, roots, nil, appdirs.SyncMarked},
		{`D:\Provider\x\y`, roots, nil, appdirs.SyncMarked},
		{`E:\anything`, roots, nil, appdirs.SyncMarked},
		{`C:\Users\b\data`, roots, nil, appdirs.SyncCannotTell},
		{`C:\Users\a\AppData\Local\app`, nil, denied, appdirs.SyncCannotTell},
		{`C:\Users\a\AppData\Local\app`, roots, nil, appdirs.SyncNone},
		{`C:\Users\a\Junction\data`, nil, nil, appdirs.SyncNone},
		{`C:\Users\a\Dedup\f`, nil, nil, appdirs.SyncNone},
		{`C:\Users\a\NotCloud\x`, nil, nil, appdirs.SyncNone},
		{`C:\Users\a\Sync Roots\data`, roots, nil, appdirs.SyncNone},
		{`D:\Provider2`, roots, nil, appdirs.SyncNone},
		{`C:\Users\a\Deep`, roots, nil, appdirs.SyncNone},
		{`C:\`, roots, nil, appdirs.SyncNone},
	} {
		var asked []string
		tag := func(p string) (uint32, error) {
			asked = append(asked, p)
			if fails[p] {
				return 0, denied
			}
			return tags[p], nil
		}
		got := appdirs.WindowsSync(c.path, tag, c.roots, c.rootsErr)
		if got != c.want {
			t.Errorf("%s, roots %q, roots error %v: %d, want %d, its folders asked %q", c.path, c.roots, c.rootsErr, got, c.want, asked)
		}
		if got == appdirs.SyncNone && (len(asked) == 0 || asked[len(asked)-1] != c.path[:3]) {
			t.Errorf("%s: none, with its folders asked %q, not each of them up to the root", c.path, asked)
		}
	}
}

// Each VolumeKind has its name, Local answers true for Local alone, and the zero VolumeInfo is
// Unknown and not local.
//
// What it costs when this fails: a caller that checks Local is told yes for Unknown, or for a
// VolumeInfo an error left unset.
func TestOnlyALocalVolumeReadsAsLocal(t *testing.T) {
	t.Parallel()
	kinds := []appdirs.VolumeKind{appdirs.Unknown, appdirs.Local, appdirs.Network, appdirs.FUSE, appdirs.SyncPlaceholder, appdirs.VolumeKind(9)}
	names := []string{"unknown", "local", "network", "fuse", "sync placeholder", "VolumeKind(9)"}
	for i, k := range kinds {
		v := appdirs.VolumeInfo{Type: "apfs", Kind: k}
		if v.Local() != (k == appdirs.Local) || k.String() != names[i] {
			t.Errorf("%d: Local %v, String %q, want %v, %q", uint8(k), v.Local(), k.String(), k == appdirs.Local, names[i])
		}
	}
	var zero appdirs.VolumeInfo
	if zero.Local() || zero.Kind != appdirs.Unknown || !slices.Contains(kinds, zero.Kind) {
		t.Errorf("the zero VolumeInfo is %+v, Local %v, want Unknown and not local", zero, zero.Local())
	}
}

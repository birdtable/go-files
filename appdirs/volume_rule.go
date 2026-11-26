package appdirs

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

// The names and numbers Volume's rules read, each from the header or system named, defined here so
// that each rule is built, and its table run, on every system.
const (
	// The f_type of each Linux file system the rule names, from linux/magic.h, and ZFS's from
	// OpenZFS's sys/fs/zfs.h. ext2, ext3 and ext4 share one.
	fuseMagic     = 0x65735546
	nfsMagic      = 0x6969
	smbMagic      = 0x517b
	cifsMagic     = 0xff534d42
	smb2Magic     = 0xfe534d42
	afsMagic      = 0x5346414f
	kafsMagic     = 0x6b414653
	cephMagic     = 0x00c36400
	codaMagic     = 0x73757245
	v9fsMagic     = 0x01021997
	ext4Magic     = 0xef53
	xfsMagic      = 0x58465342
	btrfsMagic    = 0x9123683e
	f2fsMagic     = 0xf2f52010
	tmpfsMagic    = 0x01021994
	zfsMagic      = 0x2fc12fc1
	msdosMagic    = 0x4d44
	exfatMagic    = 0x2011bab0
	bcachefsMagic = 0xca451a4e
	overlayMagic  = 0x794c7630

	// domainMark is the attribute macOS puts on the folder of a File Provider domain, which a
	// process of the account cannot set; providerMark leads the attributes the File Provider puts on
	// the items it keeps.
	domainMark   = "com.apple.file-provider-domain-id"
	providerMark = "com.apple.fileprovider."

	// The drive types of GetDriveType, from fileapi.h.
	driveRemovable = 2
	driveFixed     = 3
	driveRemote    = 4
	// The reparse tag of a Cloud Files placeholder, from winnt.h: IO_REPARSE_TAG_CLOUD and its
	// fifteen variants, IO_REPARSE_TAG_CLOUD_1 to _F, which set bits 12 to 15 of it.
	cloudTag     = 0x9000001a
	cloudTagMask = 0xffff0fff
)

// syncAnswer is what a system says of a sync provider behind a path.
type syncAnswer uint8

const (
	syncNone       syncAnswer = iota // no provider's mark, where the system shows one
	syncMarked                       // a provider's mark
	syncCannotTell                   // nothing that says either way
)

// darwinVolume is the rule of a macOS volume of the type typ, whose local-mount flag is local, at a
// path whose File Provider marks say sync. FUSE is told by its type before the flag is read, as
// macFUSE may set the flag. APFS, HFS+, FAT and exFAT are Local with the flag set, NFS, SMB, AFP,
// WebDAV, CIFS and FTP are Network whatever it says, and any other type is Unknown.
func darwinVolume(typ string, local bool, sync syncAnswer) VolumeInfo {
	v := VolumeInfo{Type: typ}
	switch {
	case strings.Contains(typ, "fuse"):
		v.Kind = FUSE
	case typ == "nfs" || typ == "smbfs" || typ == "afpfs" || typ == "webdav" || typ == "cifs" || typ == "ftp":
		v.Kind = Network
	case sync == syncMarked:
		v.Kind = SyncPlaceholder
	case sync == syncNone && local && (typ == "apfs" || typ == "hfs" || typ == "msdos" || typ == "exfat"):
		v.Kind = Local
	}
	return v
}

// darwinSync is what the File Provider's marks say of resolved, an absolute path with no link in
// it, that dataless says whether the provider evicted: marked where it did, or where resolved or a
// folder above it holds the domain's mark or a com.apple.fileprovider. attribute; cannot tell
// where the attributes of one of them cannot be listed, or where one is a CloudStorage or Mobile
// Documents folder in a Library folder and none is marked. names lists a name's attributes, no
// link followed.
func darwinSync(resolved string, dataless bool, names func(string) ([]string, error)) syncAnswer {
	if dataless {
		return syncMarked
	}
	answer := syncNone
	for p := resolved; ; p = path.Dir(p) {
		got, err := names(p)
		if err != nil {
			answer = syncCannotTell
		}
		for _, n := range got {
			if n == domainMark || strings.HasPrefix(n, providerMark) {
				return syncMarked
			}
		}
		if b := path.Base(p); (b == "CloudStorage" || b == "Mobile Documents") && path.Base(path.Dir(p)) == "Library" {
			answer = syncCannotTell
		}
		if path.Dir(p) == p {
			return answer
		}
	}
}

// linuxVolume is the rule of a Linux file system of the magic number magic, which the mount table
// names fstype. The magic number alone decides; fstype names it, or the number in hex where the
// table names none. overlay is Local: its upper layer, where writes go, cannot be NFS, which keeps
// no extended attributes for it.
func linuxVolume(magic uint32, fstype string) VolumeInfo {
	v := VolumeInfo{Type: fstype}
	if v.Type == "" {
		v.Type = fmt.Sprintf("%#x", magic)
	}
	switch magic {
	case fuseMagic:
		v.Kind = FUSE
	case nfsMagic, smbMagic, cifsMagic, smb2Magic, afsMagic, kafsMagic, cephMagic, codaMagic, v9fsMagic:
		v.Kind = Network
	case ext4Magic, xfsMagic, btrfsMagic, f2fsMagic, tmpfsMagic, zfsMagic, msdosMagic, exfatMagic, bcachefsMagic, overlayMagic:
		v.Kind = Local
	}
	return v
}

// mountType is the fstype of the mount in table, /proc/self/mountinfo's lines, whose mount point
// holds resolved, an absolute path with no link in it: the longest mount point that is resolved or
// a folder above it, the later line where two are the same, as a later mount covers an earlier one.
// It is "" where no line holds it.
func mountType(table, resolved string) string {
	best, typ := -1, ""
	for line := range strings.Lines(table) {
		f := strings.Fields(line)
		sep := slices.Index(f, "-")
		if sep < 6 || sep+1 >= len(f) {
			continue
		}
		at := unescapeMount(f[4])
		if len(at) >= best && (at == "/" || resolved == at || strings.HasPrefix(resolved, at+"/")) {
			best, typ = len(at), f[sep+1]
		}
	}
	return typ
}

// unescapeMount is a mount point as mountinfo writes it, each space, tab, newline and backslash
// as a backslash and three octal digits, read back.
func unescapeMount(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && isOctal(s[i+1]) && isOctal(s[i+2]) && isOctal(s[i+3]) {
			b.WriteByte((s[i+1]-'0')<<6 | (s[i+2]-'0')<<3 | (s[i+3] - '0'))
			i += 3
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }

// windowsSync is what the Cloud Files marks say of resolved, a DOS path with no link in it, its
// drive first (C:\Users\a): marked where it or a folder above it, up to the volume's root, carries
// a Cloud Files reparse tag, or is a sync root that roots names, matched by whole names with no
// case; cannot tell where the tag of one of them cannot be read, or rootsErr says the roots could
// not be read, and none is marked; none otherwise. tag reads a name's reparse tag, 0 for none,
// no link followed.
func windowsSync(resolved string, tag func(string) (uint32, error), roots []string, rootsErr error) syncAnswer {
	answer := syncNone
	if rootsErr != nil {
		answer = syncCannotTell
	}
	for p := resolved; ; p = windowsParent(p) {
		t, err := tag(p)
		if err != nil {
			answer = syncCannotTell
		} else if t&cloudTagMask == cloudTag {
			return syncMarked
		}
		if slices.ContainsFunc(roots, func(r string) bool { return strings.EqualFold(rootless(r), rootless(p)) }) {
			return syncMarked
		}
		if windowsParent(p) == p {
			return answer
		}
	}
}

// windowsParent is the folder above p, a DOS path, its drive first: C:\a for C:\a\b, C:\ for C:\a,
// and p itself for C:\ and for a path with no separator.
func windowsParent(p string) string {
	i := strings.LastIndexByte(p, '\\')
	switch {
	case i < 0 || i == len(p)-1:
		return p
	case i == strings.IndexByte(p, '\\'):
		return p[:i+1]
	}
	return p[:i]
}

// rootless is a DOS path as windowsSync compares it: without a \\?\ prefix or a separator at its
// end, so C:\a\ and \\?\C:\a are C:\a, and C:\ is C:.
func rootless(p string) string {
	return strings.TrimSuffix(strings.TrimPrefix(p, `\\?\`), `\`)
}

// windowsVolume is the rule of a Windows path whose NT path is nt, on a volume of the file system
// fsName and the drive type drive, whose Cloud Files marks say sync. A path through the
// multiple UNC provider or on a remote drive is Network; only NTFS, ReFS, FAT and exFAT on a
// numbered local disk volume, fixed or removable, under no sync root, are Local.
func windowsVolume(nt, fsName string, drive uint32, sync syncAnswer) VolumeInfo {
	v := VolumeInfo{Type: fsName}
	switch {
	case drive == driveRemote || strings.HasPrefix(nt, `\Device\Mup\`):
		v.Kind = Network
	case strings.Contains(strings.ToLower(fsName), "fuse"):
		v.Kind = FUSE
	case sync == syncMarked:
		v.Kind = SyncPlaceholder
	case sync == syncNone && localDisk(nt) && (drive == driveFixed || drive == driveRemovable) &&
		(fsName == "NTFS" || fsName == "ReFS" || fsName == "FAT" || fsName == "FAT32" || fsName == "exFAT"):
		v.Kind = Local
	}
	return v
}

// localDisk is whether nt, an NT path, is on a disk volume of this machine:
// \Device\HarddiskVolume, a number, then a separator.
func localDisk(nt string) bool {
	rest, ok := strings.CutPrefix(nt, `\Device\HarddiskVolume`)
	digits := len(rest) - len(strings.TrimLeft(rest, "0123456789"))
	return ok && digits > 0 && strings.HasPrefix(rest[digits:], `\`)
}

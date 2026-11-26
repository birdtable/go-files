package appdirs

import (
	"fmt"
	"io/fs"
	"path/filepath"
)

const opVolume = "volume"

// VolumeKind is what kind of volume holds a path: one whose file locks and fsync a program may
// trust, Local, or one of the kinds where they may not hold, or Unknown. The zero VolumeKind is
// Unknown, so a VolumeInfo left unset never reads as local.
type VolumeKind uint8

const (
	// Unknown is a volume the system names but Volume cannot place, or one whose sync-provider
	// marks cannot be read. It is never local.
	Unknown VolumeKind = iota
	// Local is a file system on a disk of this machine, removable disks among them, that no network,
	// FUSE daemon or sync provider stands behind.
	Local
	// Network is a file system served over the network: NFS, SMB and CIFS, AFP, WebDAV, AFS, Ceph,
	// Coda and 9p, and on Windows any remote drive.
	Network
	// FUSE is a file system served by a program in user space through FUSE: macFUSE, sshfs, rclone
	// mounts, ntfs-3g on Linux.
	FUSE
	// SyncPlaceholder is a folder a sync provider keeps, whose files it may evict and fetch again:
	// a macOS File Provider domain, iCloud Drive among them, or a Windows Cloud Files sync root.
	SyncPlaceholder
)

func (k VolumeKind) String() string {
	switch k {
	case Unknown:
		return "unknown"
	case Local:
		return "local"
	case Network:
		return "network"
	case FUSE:
		return "fuse"
	case SyncPlaceholder:
		return "sync placeholder"
	}
	return fmt.Sprintf("VolumeKind(%d)", uint8(k))
}

// VolumeInfo is what Volume says of the volume that holds a path.
type VolumeInfo struct {
	// Type is the system's own name for the file system: statfs's f_fstypename on macOS (apfs,
	// smbfs, macfuse), the mount table's fstype on Linux (ext4, nfs4, fuse.sshfs), or the magic
	// number in hex where the table names none, and the volume's file system name on Windows (NTFS,
	// ReFS, exFAT). It names; it decides nothing.
	Type string
	// Kind is the kind of volume.
	Kind VolumeKind
}

// Local is whether the volume is Local: false for every other kind, Unknown among them.
func (v VolumeInfo) Local() bool { return v.Kind == Local }

// Volume answers what kind of volume holds path, an existing file or folder, links followed, so a
// program that keeps a database or a lock file there can refuse a volume whose locks and fsync it
// may not trust, and say why. Each system is asked by its own means:
//   - On macOS, statfs's type name and its local-mount flag. A local-mount flag decides nothing
//     alone: macFUSE may set it. FUSE-T mounts through NFS or SMB to this machine, and reads as
//     Network. A path in a File Provider domain is SyncPlaceholder: the system's domain mark on the
//     path or a folder above it, a com.apple.fileprovider. attribute there, or a file the provider
//     evicted. A path below a Library/CloudStorage or Library/Mobile Documents folder that carries no
//     mark, or whose folders' attributes cannot be listed, is Unknown.
//   - On Linux, statfs's magic number, and the mount table's fstype for Type, which names FUSE's
//     subtype (fuse.sshfs, fuse.rclone). Linux keeps no sync-provider placeholders.
//   - On Windows, the volume's NT device, its drive type and its file system name, then whether the
//     path is under a Cloud Files sync root: a Cloud Files reparse tag on the path or a folder above
//     it, up to the volume's root, each opened with no link followed, or a sync root the system
//     registers that holds the path, matched by whole names with no case. A \Device\Mup path or a
//     remote drive is Network, a file system whose name holds FUSE is FUSE, a path under a sync
//     root is SyncPlaceholder, and only NTFS, ReFS, FAT and exFAT on a numbered local disk volume,
//     fixed or removable, are Local. A path whose tags or the registered roots cannot be read is
//     Unknown.
//
// Any other file system is Unknown. A path that cannot be looked at is an error, with a zero
// VolumeInfo, and a path that is not there an *fs.PathError matching fs.ErrNotExist; any other
// system is an *fs.PathError matching errors.ErrUnsupported. An error and Unknown are separate
// answers, and neither reads as local.
//
// The answer is the volume's now: a mount over the folder, or a sync provider set up later, may
// answer otherwise.
func Volume(path string) (VolumeInfo, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return VolumeInfo{}, &fs.PathError{Op: opVolume, Path: path, Err: err}
	}
	return askVolume(path, abs)
}

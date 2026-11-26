package appdirs

// The rules of Volume on each system, for the tests of package appdirs_test, which hold them on
// every system. They are built into the test binary only.

// SyncAnswer is syncAnswer, what a system says of a sync provider behind a path.
type SyncAnswer = syncAnswer

// The three syncAnswer values.
const (
	SyncNone       = syncNone
	SyncMarked     = syncMarked
	SyncCannotTell = syncCannotTell
)

// DarwinVolume is darwinVolume, Volume's rule on macOS.
func DarwinVolume(typ string, local bool, sync SyncAnswer) VolumeInfo {
	return darwinVolume(typ, local, sync)
}

// DarwinSync is darwinSync, what the File Provider's marks say of a path on macOS.
func DarwinSync(resolved string, dataless bool, names func(string) ([]string, error)) SyncAnswer {
	return darwinSync(resolved, dataless, names)
}

// LinuxVolume is linuxVolume, Volume's rule on Linux.
func LinuxVolume(magic uint32, fstype string) VolumeInfo { return linuxVolume(magic, fstype) }

// MountType is mountType, the fstype the Linux mount table gives the mount that holds a path.
func MountType(table, resolved string) string { return mountType(table, resolved) }

// WindowsSync is windowsSync, what the Cloud Files marks say of a path on Windows.
func WindowsSync(resolved string, tag func(string) (uint32, error), roots []string, rootsErr error) SyncAnswer {
	return windowsSync(resolved, tag, roots, rootsErr)
}

// WindowsVolume is windowsVolume, Volume's rule on Windows.
func WindowsVolume(nt, fsName string, drive uint32, sync SyncAnswer) VolumeInfo {
	return windowsVolume(nt, fsName, drive, sync)
}

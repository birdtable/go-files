//go:build unix && !darwin && !linux

package trust

import "os"

// mountOf reads no mount on a Unix other than macOS and Linux, where no volume shows every caller
// as the owner of every name, and only root mounts unless vfs.usermount is set, so
// no mounter is judged.
func mountOf(n node, _ string) (node, error) { return n, nil }

func mountOfFile(n node, _ *os.File) (node, error) { return n, nil }

// mountRoot is whether entry is a mount's root, by a device other than parent's.
func mountRoot(entry, parent string) (bool, error) { return onOtherDevice(entry, parent) }

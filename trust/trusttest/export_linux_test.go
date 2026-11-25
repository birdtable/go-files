package trusttest

import "golang.org/x/sys/unix"

// BoundIn is whether mountinfo, the text of /proc/self/mountinfo, shows a Bind's mount at e, as
// Bind's taken reads it, so a test of package trusttest_test reaches the split of a line. It is
// built into the test binary only.
func BoundIn(mountinfo, e string) bool {
	for _, m := range mountsIn(mountinfo) {
		if m.bound(e) {
			return true
		}
	}
	return false
}

// ErrnoOf is the errno a skip of a refused mount names, so a test of package trusttest_test
// reaches it. It is built into the test binary only.
func ErrnoOf(err error) string { return errnoOf(err) }

// DetachBy is Bind's detach of the mount numbered id, of handle fd, with unmount in place of its
// unmount through the handle and mountinfo, the text of /proc/self/mountinfo, read in place of
// the system's, so a test of package trusttest_test reaches its bound on a mount that stands
// still. It is built into the test binary only.
func DetachBy(fd int, id string, unmount func() error, mountinfo func() string) error {
	return detachBy(fd, id, unmount, func() (map[string]mount, error) { return mountsIn(mountinfo()), nil })
}

// MountID is the mount id Bind reads from statx's mask and mnt_id, err its error, so a test of
// package trusttest_test reaches a statx that fails or gives no mount id. It is built into the
// test binary only.
func MountID(mask uint32, id uint64, err error) (string, error) {
	return mountID(unix.Statx_t{Mask: mask, Mnt_id: id}, err)
}

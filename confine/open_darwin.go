package confine

import "golang.org/x/sys/unix"

// kernelOpen does not hold name beneath dirfd by itself: Open refused ".." and an absolute name.
func kernelOpen(dirfd int, name string, flag int, mode uint32) (int, error) {
	return unix.Openat(dirfd, name, flag|unix.O_NOFOLLOW_ANY, mode)
}

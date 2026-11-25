package confine

import "golang.org/x/sys/unix"

func kernelOpen(dirfd int, name string, flag int, mode uint32) (int, error) {
	return unix.Openat2(dirfd, name, &unix.OpenHow{
		Flags:   uint64(flag) | unix.O_LARGEFILE, //nolint:gosec // G115: open refuses negative flags before the kernel is asked
		Mode:    uint64(mode),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
}

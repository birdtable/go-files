package confine

import (
	"errors"

	"golang.org/x/sys/unix"
)

func renameAt(fromfd int, from string, tofd int, to string, exchange bool) error {
	flag := uint32(unix.RENAME_EXCL)
	if exchange {
		flag = unix.RENAME_SWAP
	}
	return unix.RenameatxNp(fromfd, from, tofd, to, flag)
}

// unsupported is whether err is renameatx_np's answer where the file system does not take the
// flag. Its EINVAL is the caller's own: a folder moved into itself.
func unsupported(err error) bool {
	return errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP)
}

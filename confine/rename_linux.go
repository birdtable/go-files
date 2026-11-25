package confine

import (
	"errors"

	"golang.org/x/sys/unix"
)

func renameAt(fromfd int, from string, tofd int, to string, exchange bool) error {
	flag := uint(unix.RENAME_NOREPLACE)
	if exchange {
		flag = unix.RENAME_EXCHANGE
	}
	return unix.Renameat2(fromfd, from, tofd, to, flag)
}

// unsupported is whether err is renameat2's answer where the kernel has no renameat2 (ENOSYS), or
// the file system does not take the flag (EINVAL, or EOPNOTSUPP from some).
func unsupported(err error) bool {
	return errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EOPNOTSUPP)
}

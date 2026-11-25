package confine_test

import (
	"errors"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// callsPerSwap is how many calls of swapper's func trade d and l: one renameatx_np.
const callsPerSwap = 1

// swapper swaps in's d and l, each call in one renameatx_np.
func swapper(in string) func() error {
	d, l := filepath.Join(in, "d"), filepath.Join(in, "l")
	return func() error { return unix.RenameatxNp(unix.AT_FDCWD, d, unix.AT_FDCWD, l, unix.RENAME_SWAP) }
}

// refusedHere is whether err is a raw ENOTDIR or ELOOP, which the issue counts as a refusal on
// macOS.
func refusedHere(err error) bool { return errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.ELOOP) }

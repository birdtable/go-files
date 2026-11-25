package confine_test

import (
	"path/filepath"

	"golang.org/x/sys/unix"
)

// callsPerSwap is how many calls of swapper's func trade d and l: one renameat2.
const callsPerSwap = 1

// swapper swaps in's d and l, each call in one renameat2.
func swapper(in string) func() error {
	d, l := filepath.Join(in, "d"), filepath.Join(in, "l")
	return func() error { return unix.Renameat2(unix.AT_FDCWD, d, unix.AT_FDCWD, l, unix.RENAME_EXCHANGE) }
}

// refusedHere is false: Linux's answers all come as ErrOutside.
func refusedHere(error) bool { return false }

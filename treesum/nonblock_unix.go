//go:build unix

package treesum

import "syscall"

// nonblock opens a FIFO swapped in since the look with no wait on a writer.
const nonblock = syscall.O_NONBLOCK

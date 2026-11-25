//go:build unix

package nowait

import "syscall"

// noWait opens a pipe at the name with no wait for its other end, so an open never hangs on one;
// a regular file or a folder reads and writes as without.
const noWait = syscall.O_NONBLOCK

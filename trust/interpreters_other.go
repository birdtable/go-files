//go:build unix && !linux && !darwin

package trust

import "syscall"

// kernel refuses every script: how this system reads a #! line is not proven here.
func kernel() shebang { return unprovenShebang() }

// openNoWait opens a name without waiting on a pipe or taking a terminal.
const openNoWait = syscall.O_NONBLOCK | syscall.O_NOCTTY

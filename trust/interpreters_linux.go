package trust

import "syscall"

// kernel is how this system reads a #! line.
func kernel() shebang { return linuxShebang() }

// openNoWait opens a name without waiting on a pipe or taking a terminal.
const openNoWait = syscall.O_NONBLOCK | syscall.O_NOCTTY

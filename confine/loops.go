package confine

import (
	"errors"
	"syscall"
)

// loops is whether err is ELOOP: os.Root's answer when a link it met is gone before it reads it.
func loops(err error) bool { return errors.Is(err, syscall.ELOOP) }

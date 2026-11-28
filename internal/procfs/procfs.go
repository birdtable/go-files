// Package procfs holds Linux's /proc to a procfs's own root, and opens a file beneath that root
// crossing no mount, so what is read there is the kernel's: an owner of this mount namespace may
// mount over /proc, or over a folder or file under it, or bind another file of a procfs there, and
// serve what it likes. trust reads self/mountinfo and confine reads self/fd this way, rather than
// each hold /proc by itself.
// It imports the standard library, and on Linux golang.org/x/sys.
package procfs

import "errors"

// ErrNotProc is a file read under /proc that is not held to this process's procfs. A caller wraps it
// with the name and the step it was taking.
var ErrNotProc = errors.New("is not held to procfs's own file, so what it holds is not the kernel's")

package trust

import (
	"errors"
	"io/fs"
)

// readProc opens nothing and answers errors.ErrUnsupported: Windows has no /proc, and an open of
// its name by os would pass no quality of service, so a junction planted on the way to a pipe
// would let the pipe's server act as the caller. overflow asks it only on Linux.
func readProc(path string) ([]byte, error) {
	return nil, &fs.PathError{Op: "open", Path: path, Err: errors.ErrUnsupported}
}

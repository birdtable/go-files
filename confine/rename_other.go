//go:build !linux && !darwin && !windows

package confine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const stripsTrail, swaps = false, false

// rename refuses, and nothing moves: a look before a plain rename could lose a race and replace
// what another process put there. renameIn refuses a swap.
func rename(_ *os.Root, from, _ string, _ bool) error {
	return &fs.PathError{Op: opRename, Path: filepath.ToSlash(from),
		Err: fmt.Errorf("this system has no rename that refuses to replace a name and follows no link, and nothing was moved: %w", errors.ErrUnsupported)}
}

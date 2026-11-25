//go:build linux || darwin || windows

package confine

import (
	"fmt"
	"io/fs"
	"path/filepath"
)

func lastRefused(name, what string) error {
	return &fs.PathError{Op: opRename, Path: filepath.ToSlash(name), Err: fmt.Errorf("it is %s, and no rename acts on it: %w", what, ErrOutside)}
}

// taken refuses a name that must be missing, or there when exchange, and is not.
func taken(to string, exchange bool) error {
	if exchange {
		return &fs.PathError{Op: opRename, Path: filepath.ToSlash(to), Err: fs.ErrNotExist}
	}
	return &fs.PathError{Op: opRename, Path: filepath.ToSlash(to), Err: fs.ErrExist}
}

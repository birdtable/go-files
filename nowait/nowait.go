// Package nowait opens a file by its path as os.OpenFile does, and never waits for a pipe's other
// end. That is all it adds. It follows a link at any component, the last included, as os.OpenFile
// does, holds the path beneath no folder, and checks nothing of what it opened: a program that
// must follow no link opens through confine instead.
//
// Built on os.OpenFile, with O_NONBLOCK on Unix. The program configures the path, the flags and
// the mode. It imports the standard library alone.
package nowait

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Open opens name, a clean absolute path, as os.OpenFile does, and never waits: on Unix it is
// made with O_NONBLOCK, so a pipe at name opens at once to be read, or is refused at once (ENXIO)
// to be written with no reader. A regular file or a folder reads and writes as without. It follows
// a link at any component, as os.OpenFile does, and checks nothing of what it opened, so the
// caller checks what it got. A name that is not absolute and clean is refused, matching
// fs.ErrInvalid, and nothing is opened.
func Open(name string, flag int, perm fs.FileMode) (*os.File, error) {
	if !filepath.IsAbs(name) || filepath.Clean(name) != name {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fmt.Errorf("it is not a clean absolute path: %w", fs.ErrInvalid)}
	}
	return os.OpenFile(name, flag|noWait, perm) //nolint:gosec // G304: the caller names the path, which is what Open is for
}

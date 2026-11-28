package confine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// pipesDevice is the NT name of the pipes' device, below which each name is a named pipe.
const pipesDevice = `\Device\NamedPipe`

// rootOnPipes refuses root, before any open beneath it, where its folder, read from its own handle
// by GetFinalPathNameByHandle, is on the pipes' device, as an os.OpenRoot through a junction to the
// pipes' folder is: a name below it opens a pipe by Go's open within a root, which passes no quality
// of service, so the pipe's server could act as the caller. A root anywhere else passes. The system
// reads no final path of a root on the pipes' device (GetFinalPathNameByHandle answers "Incorrect
// function."), so where the path cannot be read, the root's handle's own file type decides: a root on
// a disk passes, and any other is refused as a device or a pipe. name is the name the refusal is
// about.
func rootOnPipes(root *os.Root, name string) (err error) {
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	h := windows.Handle(f.Fd())
	var buf [windows.MAX_LONG_PATH]uint16
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), volumeNameNT)
	if err != nil {
		ft, ferr := windows.GetFileType(h)
		if ferr == nil {
			return nil
		}
		return &fs.PathError{Op: opOpen, Path: filepath.ToSlash(name), Err: fmt.Errorf("the root is of file type %d, not on a disk, and its own path cannot be read (%w), so it names a device or a pipe, and nothing beneath it was opened: %w", ft, errors.Join(err, ferr), fs.ErrInvalid)}
	}
	final := windows.UTF16ToString(buf[:min(int(n), len(buf))])
	if !strings.EqualFold(final, pipesDevice) && !strings.HasPrefix(strings.ToLower(final), strings.ToLower(pipesDevice+`\`)) {
		return nil
	}
	return &fs.PathError{Op: opOpen, Path: filepath.ToSlash(name), Err: fmt.Errorf("the root is %s, on the pipes' device, where a name names a device or a pipe, and nothing beneath it was opened: %w", final, fs.ErrInvalid)}
}

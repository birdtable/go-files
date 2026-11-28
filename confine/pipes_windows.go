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

// rootOnPipes refuses root, before any open beneath it, where its folder is a host's pipes: on the
// pipes' device, as an os.OpenRoot through a junction to the pipes' folder is, or on a host's pipes
// share, as onPipesShare reads it from the root's own name or its final path. A name below such a
// root opens a pipe by Go's open within a root, which passes no quality of service, so the pipe's
// server could act as the caller. Each confine call that opens a name beneath a caller's root asks
// it first. A root anywhere else passes, a root on any other share among them. The folder is read
// from the root's own handle by GetFinalPathNameByHandle. The system reads no final path of a root
// on the pipes' device (GetFinalPathNameByHandle answers "Incorrect function."), so where the path
// cannot be read, the root's handle's own file type decides: a root on a disk passes, and any other
// is refused as a device or a pipe. name is the name the refusal is about.
func rootOnPipes(root *os.Root, name string) (err error) {
	refused := func(why string, args ...any) error {
		return &fs.PathError{Op: opOpen, Path: filepath.ToSlash(name), Err: fmt.Errorf("the root "+why+", where a name names a device or a pipe, and nothing beneath it was opened: %w", append(args, fs.ErrInvalid)...)}
	}
	if onPipesShare(root.Name()) {
		return refused("is %s, a host's pipes share", root.Name())
	}
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
		if ferr == nil && ft == windows.FILE_TYPE_DISK {
			return nil
		}
		return refused("is of file type %d, not on a disk, and its own path cannot be read (%w)", ft, errors.Join(err, ferr))
	}
	final := windows.UTF16ToString(buf[:min(int(n), len(buf))])
	switch {
	case strings.EqualFold(final, pipesDevice) || strings.HasPrefix(strings.ToLower(final), strings.ToLower(pipesDevice+`\`)):
		return refused("is %s, on the pipes' device", final)
	case onPipesShare(final):
		return refused("is %s, on a host's pipes share", final)
	}
	return nil
}

// onPipesShare says whether path is on a host's pipes share, IPC$ or pipe, compared without case,
// for any host, localhost and a dotted name or address among them. path is a UNC path in any of its
// spellings (\\host\share, //host/share, \\?\UNC\host\share, \\.\UNC\host\share, \??\UNC\host\share)
// or a final path under \Device\Mup\, whose components that start with ';', a redirector's own,
// are passed over to reach the host. A path with no share is not on one.
func onPipesShare(path string) bool {
	p := strings.ToLower(strings.ReplaceAll(path, `/`, `\`))
	for _, prefix := range []string{`\device\mup\`, `\\?\unc\`, `\\.\unc\`, `\??\unc\`, `\\`} {
		rest, ok := strings.CutPrefix(p, prefix)
		if !ok {
			continue
		}
		parts := strings.Split(rest, `\`)
		for len(parts) > 0 && strings.HasPrefix(parts[0], ";") {
			parts = parts[1:]
		}
		return len(parts) > 1 && (parts[1] == `ipc$` || parts[1] == `pipe`)
	}
	return false
}

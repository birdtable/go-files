package winapi

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

// volumeNameNT is GetFinalPathNameByHandle's VOLUME_NAME_NT, from fileapi.h: the path as the NT
// object manager names it, \Device\HarddiskVolume3\..., \Device\Mup\... or \Device\NamedPipe.
const volumeNameNT = 0x2

// RootOnPipes refuses root, before any open beneath it, where its folder is a host's pipes: on the
// pipes' device, as an os.OpenRoot through a junction to the pipes' folder is, or on a host's pipes
// share, as PipesShare reads it from the root's own name, before any open, or from its final
// path (OnPipes). A name below such a root opens a pipe by Go's open within a root, which passes no
// quality of service, so the pipe's server could act as the caller. Each call of confine, lock and
// write that opens a name beneath a caller's root asks it first. A root anywhere else passes, a root
// on any other share among them. name is the name the refusal is about. A refusal is an
// *fs.PathError, op "open", matching fs.ErrInvalid.
func RootOnPipes(root *os.Root, name string) (err error) {
	if PipesShare(root.Name()) {
		return onPipes(name, "is %s, a host's pipes share", root.Name())
	}
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	return OnPipes(f, name)
}

// OnPipes refuses the folder d, opened to open names in it, where it is on the pipes' device or on
// a host's pipes share, read from d's own handle by GetFinalPathNameByHandle. The system reads no
// final path of a folder on the pipes' device (GetFinalPathNameByHandle answers "Incorrect
// function."), nor of one on a pipes share, so where the path cannot be read the handle's own file
// type decides: a folder on a disk passes, and any other is refused as a device or a pipe. Every
// make here asks it of the folder it makes in. name is the name the refusal is about. A refusal is
// an *fs.PathError, op "open", matching fs.ErrInvalid.
func OnPipes(d *os.File, name string) error {
	h := windows.Handle(d.Fd())
	var buf [windows.MAX_LONG_PATH]uint16
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), volumeNameNT)
	if err != nil {
		ft, ferr := windows.GetFileType(h)
		if ferr == nil && ft == windows.FILE_TYPE_DISK {
			return nil
		}
		return onPipes(name, "is of file type %d, not on a disk, and its own path cannot be read (%w)", ft, errors.Join(err, ferr))
	}
	final := windows.UTF16ToString(buf[:min(int(n), len(buf))])
	switch {
	case strings.EqualFold(final, pipesDevice) || strings.HasPrefix(strings.ToLower(final), strings.ToLower(pipesDevice+`\`)):
		return onPipes(name, "is %s, on the pipes' device", final)
	case PipesShare(final):
		return onPipes(name, "is %s, on a host's pipes share", final)
	}
	return nil
}

// onPipes is the refusal of RootOnPipes and OnPipes: why says what the folder is.
func onPipes(name, why string, args ...any) error {
	return &fs.PathError{Op: "open", Path: filepath.ToSlash(name), Err: fmt.Errorf("the folder "+why+", where a name names a device or a pipe, and nothing beneath it was opened: %w", append(args, fs.ErrInvalid)...)}
}

// PipesShare reports whether path is on a host's pipes share, IPC$ or pipe, where a name opens that
// host's named pipe: compared without case, for any host, localhost and a dotted name or address
// among them. path is a UNC path in any of its spellings (\\host\share, //host/share,
// \\?\UNC\host\share, \\.\UNC\host\share, \??\UNC\host\share), or a path under the multiple UNC
// provider's device (\Device\Mup\, and \\?\GLOBALROOT\Device\Mup\ with each of the device
// prefixes), as a final path read from a handle is, whose components that start with ';', a
// redirector's own, are passed over to reach the host. Empty components are passed over, and a
// share name's trailing dots and spaces, which Win32 strips from a name, are not read, so
// \\host\\IPC$ and \\host\IPC$. are on one. A path with no share is not on one.
func PipesShare(path string) bool {
	p := strings.ToLower(strings.ReplaceAll(path, `/`, `\`))
	for _, prefix := range []string{`\device\mup\`, `\\?\unc\`, `\\.\unc\`, `\??\unc\`, `\\`} {
		rest, ok := strings.CutPrefix(p, prefix)
		if !ok {
			continue
		}
		var parts []string
		for part := range strings.SplitSeq(rest, `\`) {
			if len(parts) > 0 || !strings.HasPrefix(part, ";") {
				parts = append(parts, part)
			}
		}
		if len(parts) < 2 {
			return false
		}
		share := parts[1]
		return share == `ipc$` || share == `pipe`
	}
	return false
}

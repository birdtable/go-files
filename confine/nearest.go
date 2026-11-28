package confine

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
)

// Nearest answers the nearest folder, at start or above it, that holds marker, as an absolute
// path. marker is taken as Local takes it and may be several names (".app/pin.yaml"); an empty one
// is refused too, matching ErrOutside. start is made absolute and clean and walked up by its own
// names: no link in it is resolved, so pass Resolve(start) for the walk of the real folders. Each
// folder is asked with os.Lstat, on Windows by an open of its own that asks for identification
// alone, so a marker that is a link counts, a dangling one too. A marker that is a pipe, a device
// or a socket is no file or folder: it stops the walk, refused matching fs.ErrInvalid. On Windows a
// start that names a device or a pipe, not a path on a drive or a volume, is refused before any
// look, matching fs.ErrInvalid and ErrOutside, read as given and again once absolute, as Go takes
// a \??\ path for one on the working drive. A folder
// without marker, or whose marker's inner name is not a folder, sends the walk up; any other error
// stops it and is returned, so a folder that cannot be read is never taken for one with no marker.
// top is the last folder asked, "" the volume's top (/, C:\, \\host\share\). A top that is not
// start or above it, spelled as the walk spells it once absolute (on Windows in the same case too),
// is refused before any look, matching ErrOutside. With no marker, an *fs.PathError naming marker,
// matching fs.ErrNotExist. The answer is not judged: a marker in a folder another account owns or
// may write is found like any other (git's CVE-2022-24765). A caller that will confine reads and
// writes to it hands it to trust.Path(dir, trust.Folder, who) first, or names a top it trusts.
func Nearest(start, marker, top string) (string, error) { return nearest(start, marker, top, sysLstat) }

func nearest(start, marker, top string, look func(string) (fs.FileInfo, error)) (string, error) {
	name, err := Local(marker)
	if err == nil && marker == "" {
		err = fmt.Errorf("an empty marker names no file: %w", ErrOutside)
	}
	if err != nil {
		return "", err
	}
	if err := notDevice("nearest", start); err != nil {
		return "", err
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	if err := notDevice("nearest", dir); err != nil {
		return "", err
	}
	walk := []string{dir}
	for up := filepath.Dir(dir); up != dir; up = filepath.Dir(dir) {
		dir = up
		walk = append(walk, dir)
	}
	if top != "" {
		if walk, err = upTo(walk, top); err != nil {
			return "", err
		}
	}
	for _, d := range walk {
		fi, err := look(filepath.Join(d, name))
		if err == nil && fi.Mode().Type()&(fs.ModeNamedPipe|fs.ModeDevice|fs.ModeCharDevice|fs.ModeSocket) != 0 {
			return "", &fs.PathError{Op: "nearest", Path: filepath.Join(d, name), Err: fmt.Errorf("the marker is a pipe, a device or a socket, no file or folder: %w", fs.ErrInvalid)}
		}
		if err == nil {
			return d, nil
		}
		if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, errNotDir) {
			return "", err
		}
	}
	return "", &fs.PathError{Op: "nearest", Path: marker, Err: fs.ErrNotExist}
}

// upTo cuts walk, start's folders from it up, after top, or refuses top as not among them.
func upTo(walk []string, top string) ([]string, error) {
	stop, err := filepath.Abs(top)
	if err != nil {
		return nil, err
	}
	for i, d := range walk {
		if d == stop {
			return walk[:i+1], nil
		}
	}
	return nil, &fs.PathError{Op: "nearest", Path: top, Err: fmt.Errorf("it is not %s or a folder above it: %w", walk[0], ErrOutside)}
}

package appdirs

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// askVolume asks the volume of path, made absolute as abs, its type and its local-mount flag, and
// the path and each folder above it their File Provider marks, every link resolved first.
func askVolume(path, abs string) (VolumeInfo, error) {
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return VolumeInfo{}, &fs.PathError{Op: opVolume, Path: path, Err: err}
	}
	var st unix.Statfs_t
	var ls unix.Stat_t
	if err := unix.Statfs(resolved, &st); err != nil {
		return VolumeInfo{}, &fs.PathError{Op: opVolume, Path: path, Err: err}
	}
	if err := unix.Lstat(resolved, &ls); err != nil {
		return VolumeInfo{}, &fs.PathError{Op: opVolume, Path: path, Err: err}
	}
	sync := darwinSync(resolved, ls.Flags&unix.SF_DATALESS != 0, attributeNames)
	return darwinVolume(unix.ByteSliceToString(st.Fstypename[:]), st.Flags&unix.MNT_LOCAL != 0, sync), nil
}

// attributeNames lists the extended attributes of the name p, no link followed, asking again
// where they grew between the size and the read.
func attributeNames(p string) ([]string, error) {
	for range 4 {
		n, err := unix.Llistxattr(p, nil)
		if err != nil || n == 0 {
			return nil, err
		}
		buf := make([]byte, n)
		n, err = unix.Llistxattr(p, buf)
		if errors.Is(err, unix.ERANGE) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return strings.Split(strings.TrimSuffix(string(buf[:n]), "\x00"), "\x00"), nil
	}
	return nil, unix.ERANGE
}

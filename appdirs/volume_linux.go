package appdirs

import (
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// askVolume asks the file system of path, made absolute as abs, its magic number, and the mount
// table its name, every link resolved first. A table that cannot be read leaves Type the number.
func askVolume(path, abs string) (VolumeInfo, error) {
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return VolumeInfo{}, &fs.PathError{Op: opVolume, Path: path, Err: err}
	}
	var st unix.Statfs_t
	if err := unix.Statfs(resolved, &st); err != nil {
		return VolumeInfo{}, &fs.PathError{Op: opVolume, Path: path, Err: err}
	}
	typ := ""
	if table, err := os.ReadFile("/proc/self/mountinfo"); err == nil {
		typ = mountType(string(table), resolved)
	}
	return linuxVolume(uint32(st.Type), typ), nil //nolint:gosec // G115: a magic number is the low 32 bits of f_type
}

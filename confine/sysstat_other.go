//go:build !windows

package confine

import (
	"io/fs"
	"os"
)

// sysStat is os.Stat, which on Windows is winapi.Stat.
func sysStat(path string) (fs.FileInfo, error) { return os.Stat(path) }

// sysLstat is os.Lstat, which on Windows is winapi.Lstat.
func sysLstat(path string) (fs.FileInfo, error) { return os.Lstat(path) }

// openFolderPath opens root read-only, never waiting on a pipe.
func openFolderPath(root string) (*os.File, error) {
	return os.OpenFile(root, os.O_RDONLY|noWait, 0) //nolint:gosec // G304: Folds reads the folder its caller names, and asks this descriptor
}

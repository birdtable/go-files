//go:build !windows

package appdirs

import (
	"io/fs"
	"os"
)

// sysStat is os.Stat, which on Windows is winapi.Stat.
func sysStat(path string) (fs.FileInfo, error) { return os.Stat(path) }

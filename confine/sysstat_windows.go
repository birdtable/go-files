package confine

import (
	"io/fs"
	"os"

	"github.com/birdtable/go-files/internal/winapi"
)

// sysStat is os.Stat by opens that let no pipe's server act as the caller (winapi.Stat): a path
// whose links lead to a pipe gives its server who the caller is and nothing it can act as.
func sysStat(path string) (fs.FileInfo, error) { return winapi.Stat(path) }

// sysLstat is os.Lstat by opens that let no pipe's server act as the caller (winapi.Lstat).
func sysLstat(path string) (fs.FileInfo, error) { return winapi.Lstat(path) }

// openFolderPath opens root, links followed, for its attributes alone, by opens that let no pipe's
// server act as the caller (winapi.OpenPath): a pipe is never waited on, as no open of a name waits
// for one on Windows.
func openFolderPath(root string) (*os.File, error) { return winapi.OpenPath(root, true) }

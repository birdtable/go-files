package appdirs

import (
	"io/fs"

	"github.com/birdtable/go-files/internal/winapi"
)

// sysStat is os.Stat by opens that let no pipe's server act as the caller (winapi.Stat): a path
// whose links lead to a pipe gives its server who the caller is and nothing it can act as.
func sysStat(path string) (fs.FileInfo, error) { return winapi.Stat(path) }

//go:build unix

package appdirs

import (
	"os"
	"path/filepath"
	"strconv"
)

// tempName is the folder of app under the system's temp folder, named for this account.
func tempName(app string) string { return app + "-" + strconv.Itoa(os.Geteuid()) }

// realPath is p with every link on its way resolved.
func realPath(p string) (string, error) { return filepath.EvalSymlinks(p) }

// newFolder makes the temp folder dir, 0700 less the umask, which narrows it alone; trust judges it.
func newFolder(dir string, _ bool) error { return os.Mkdir(dir, 0o700) }

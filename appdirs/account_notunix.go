//go:build !unix

package appdirs

import (
	"errors"
	"os/user"
)

// account is, on Windows, the home of the process's token's account, which os/user reads as the
// token's profile folder and never from USERPROFILE. Each other system but Unix, whose thisKind is
// not windowsKind, is refused: os/user reads the home there from the environment, $home on plan9
// and HOME on js and wasip1.
func account() (string, error) {
	if thisKind != windowsKind {
		return "", errors.New("this system says an account's home only through the environment")
	}
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
}

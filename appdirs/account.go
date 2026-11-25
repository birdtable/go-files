package appdirs

import (
	"errors"
	"fmt"
	"path/filepath"
)

// ErrNoAccountHome is AccountHome's refusal: the system did not say the running account's home,
// or said one that is empty, not an absolute path, or not clean. Experimental.
var ErrNoAccountHome = errors.New("appdirs: the system does not say this account's home")

// AccountHome is the home folder of the account that runs the process, as the system's account
// records keep it, and never as the environment says: no variable is read, HOME and USERPROFILE
// among them, so a home a test or a caller set in the environment never answers. On Unix it is the
// home of the real user ID, os.Getuid. Where os/user asks the C library, which a build with cgo does
// and macOS does through libSystem, it is os/user's LookupId of that uid. Where os/user falls back
// to HOME instead, a build without cgo off macOS, android, or a build with the osusergo tag,
// os/user is not called: on macOS that is ErrNoAccountHome, since its accounts are not in
// /etc/passwd, and elsewhere AccountHome reads /etc/passwd itself. On Windows it is the profile
// folder of the process's token, as os/user's Current reads it. Where the system cannot say, it
// is ErrNoAccountHome, matched by errors.Is, with the cause: a uid the records do not hold, an
// account found only in LDAP or sssd by a build that reads /etc/passwd, a line of /etc/passwd that
// is not seven fields, a home that is empty, relative or not clean, and every system but Unix and
// Windows, whose lookup falls back to the environment. The answer is absolute, clean and never
// empty. Nothing is made, and the folder is not opened. Experimental.
func AccountHome() (string, error) {
	return homeOf(account())
}

// homeOf is AccountHome's answer from the lookup's: its error, or a home that is empty, relative
// or that filepath.Clean would change, is ErrNoAccountHome.
func homeOf(home string, err error) (string, error) {
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNoAccountHome, err)
	}
	if !filepath.IsAbs(home) || filepath.Clean(home) != home {
		return "", fmt.Errorf("home %q: %w", home, ErrNoAccountHome)
	}
	return home, nil
}

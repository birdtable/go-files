//go:build unix && ((!cgo && !darwin) || android || osusergo)

package appdirs

import (
	"errors"
	"os"
)

// byUID is the home of uid's account as /etc/passwd gives it, read here and not by os/user. The
// build constraint is that of go1.27's os/user/lookup_stubs.go, go.mod's Go, on Unix: in this
// build os/user's Current falls back to HOME where /etc/passwd does not hold the account, and
// LookupId asks Current first, so neither is called. macOS, whose accounts are in
// Directory Services and not in /etc/passwd, is refused.
func byUID(uid int) (string, error) {
	if thisKind == darwinKind {
		return "", errors.New("macOS keeps its accounts outside /etc/passwd, and this build does not ask the system")
	}
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return "", err
	}
	home, err := homeInPasswd(f, uid)
	return home, errors.Join(err, f.Close())
}

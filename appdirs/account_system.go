//go:build (cgo || darwin) && !osusergo && unix && !android

package appdirs

import (
	"os/user"
	"strconv"
)

// byUID is the home of uid's account as the C library gives it, through os/user's LookupId. The
// build constraint is that of go1.27's os/user/cgo_lookup_unix.go, go.mod's Go: in this build
// os/user's Current, which LookupId asks first, comes from the C library too, and no lookup reads
// a variable. Every other Unix build is account_passwd.go's.
func byUID(uid int) (string, error) {
	u, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
}

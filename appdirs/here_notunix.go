//go:build !unix || darwin || hurd

package appdirs

// Each system but the Unix ones here_unix.go is built for: Here lays out no XDG folders.
const (
	xdgPart = int(noKind)
	xdgHome = ""
)

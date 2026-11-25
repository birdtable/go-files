//go:build unix && !darwin && !hurd

package appdirs

// Each Unix system For names but macOS, by this file's constraint: Here lays out the XDG folders.
// hurd, which Go counts as Unix, is not one For names.
const (
	xdgPart = int(xdgKind)
	xdgHome = "HOME" // the variable os.UserHomeDir reads the home from here
)

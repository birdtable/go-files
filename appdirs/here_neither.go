//go:build !windows && (!darwin || ios)

package appdirs

// Each system but Windows and macOS, by this file's constraint: Here lays out neither's folders.
// here_unix.go gives the XDG systems' home, and a system For names no folders for, plan9 among
// them, has none.
const (
	windowsPart = int(noKind)
	darwinPart  = int(noKind)
	windowsHome = ""
	darwinHome  = ""
)

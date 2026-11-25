//go:build darwin && !ios

package appdirs

// macOS, by this file's name and constraint: ios builds darwin's files, and For names it no folders.
const (
	windowsPart = int(noKind)
	darwinPart  = int(darwinKind)
	windowsHome = ""
	darwinHome  = "HOME" // the variable os.UserHomeDir reads the home from here
)

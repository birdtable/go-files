package appdirs

// Windows, by this file's name: Here lays out Windows' folders, and not macOS's.
const (
	windowsPart = int(windowsKind)
	darwinPart  = int(noKind)
	windowsHome = "USERPROFILE" // the variable os.UserHomeDir reads the home from here
	darwinHome  = ""
)

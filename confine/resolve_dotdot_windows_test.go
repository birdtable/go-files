package confine_test

// On Windows the system takes a .. in a relative link target by its spelling, against the link's
// own folder: tree/l -> d\..\x is tree\x whatever d is, tree/updown -> gone\..\x is tree\x, and
// tree/fx -> x\..\sub is tree\sub though x is a file. The system's own look at each path, held
// against the walk's answer, is what proves it here.
const (
	dotDotAfterLink    = "tree/x"
	dotDotAfterMissing = "tree/x"
	dotDotAfterFile    = "tree/sub"
	dotDotBelow        = "x"
)

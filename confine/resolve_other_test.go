//go:build !windows

package confine_test

// On Linux, macOS and the other Unix systems the kernel takes a .. in a link's target from the
// folder the walk reached: tree/l -> d/../x, d a link to out/sub, is out/x, outside tree; a ..
// after a missing name cannot be walked, and tree/fx -> x/../sub, x a file, is ENOTDIR at x.
const (
	dotDotAfterLink    = "out/x"
	dotDotAfterMissing = ""
	dotDotAfterFile    = ""
	dotDotBelow        = ""
)

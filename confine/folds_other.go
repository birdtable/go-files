//go:build !darwin && !linux && !windows

package confine

import "os"

// askFolding is ErrNoFolding on a system Folds does not ask.
func askFolding(_ *os.File, root string) (Folding, error) {
	return FoldUnknown, unknownFolding(root, "this system's", "is not one Folds asks")
}

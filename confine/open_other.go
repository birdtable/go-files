//go:build !linux && !darwin

package confine

import (
	"io/fs"
	"os"
)

func open(root *os.Root, name string, flag int, perm fs.FileMode, reached func(step string)) (*os.File, error) {
	return walkOpen(root, name, flag, perm, reached)
}

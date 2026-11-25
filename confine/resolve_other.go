//go:build !windows

package confine

import (
	"io/fs"
	"os"
)

// bySpelling is false: a .. in a link's target goes up from the folder the walk reached, as the
// kernel walks it.
const bySpelling = false

// readLink answers the target of the link at next and whether the walk follows it: a symbolic
// link, and nothing else, is a link here.
func readLink(next string, fi fs.FileInfo) (string, bool, error) {
	if fi.Mode()&fs.ModeSymlink == 0 {
		return "", false, nil
	}
	target, err := os.Readlink(next)
	return target, err == nil, err
}

// asTaken is path as written: the walk takes each . and .. where it comes, as the kernel does.
func asTaken(path string) (string, error) { return path, nil }

// cleanAbs takes every absolute target: the walk goes up at each .. as the kernel does.
func cleanAbs(string, string) error { return nil }

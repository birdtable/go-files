//go:build linux || darwin || freebsd

package confine

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// link makes at a symbolic link to target by symlinkat, from at's folder opened by Open's own
// call, the kernel's on Linux and macOS and the walk on FreeBSD, so no link is followed on the way
// to it, and none at its last component: symlinkat makes a name or refuses one that is there.
func link(root *os.Root, target, at string) error {
	dir, err := open(root, filepath.Dir(at), os.O_RDONLY|unix.O_DIRECTORY, 0, func(string) {})
	if err != nil {
		return err
	}
	err = unix.Symlinkat(target, int(dir.Fd()), filepath.Base(at))
	for errors.Is(err, unix.EINTR) {
		err = unix.Symlinkat(target, int(dir.Fd()), filepath.Base(at))
	}
	if err != nil {
		return errors.Join(madeErr(at, err), dir.Close())
	}
	return dir.Close()
}

// absoluteLinks: a link CopyTree makes holds its target relative to its own folder.
const absoluteLinks = false

// fits takes every target: a symbolic link's length is the kernel's to refuse.
func fits(string) error { return nil }

// linkFile makes at a symbolic link to the file target, as link does.
func linkFile(root *os.Root, target, at string) error { return link(root, target, at) }

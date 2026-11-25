package confine

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

// PlantJunction is plantJunction, for the tests of package confine_test. It is built into the test
// binary only.
func PlantJunction(t *testing.T, target, link string) {
	t.Helper()
	plantJunction(t, target, link)
}

// LinkFolderSet is LinkFolder past its checks of at and target, with set run in place of the
// system's set of the mount point, once the folder is made and held. It is built into the test
// binary only.
func LinkFolderSet(root *os.Root, target, at string, set func() error) error {
	return linkWith(root, target, at, folderLink(func(windows.Handle, []byte) error { return set() }))
}

// PlantRawJunction is plantRawJunction, for the tests of package confine_test: link made a junction
// whose substitute name is sub and whose print name is shown, as written. It is built into the
// test binary only.
func PlantRawJunction(link, sub, shown string) error {
	return plantRawJunction(link, sub, shown)
}

// lxSymlink is IO_REPARSE_TAG_LX_SYMLINK, the tag of a link WSL makes: a name surrogate, so Go
// reports it as irregular, and neither a link nor a junction, so os.Readlink does not read it.
const lxSymlink = 0xa000001d

// PlantOtherReparse makes link a new folder holding a reparse point that is neither a link nor a
// junction, WSL's link to x, and answers the system's error where it refuses to set one. It is
// built into the test binary only.
func PlantOtherReparse(link string) error {
	return plantReparse(link, func(h windows.Handle) error {
		return setReparse(h, lxSymlink, append([]byte{2, 0, 0, 0}, 'x')) // version 2, then the target in UTF-8
	})
}

// LinkFileSet is LinkFile past its checks of at and target, with set run in place of the system's
// set of the symbolic link, once the file is made and held, and its answer read as setSymlink
// reads the system's. It is built into the test binary only.
func LinkFileSet(root *os.Root, target, at string, set func() error) error {
	return linkWith(root, target, at, fileLink(func(windows.Handle, []byte) error { return symlinkErr(set()) }))
}

// Listed is listed, for the tests of package confine_test. It is built into the test binary only.
func Listed(final, pathext string) bool { return listed(final, pathext) }

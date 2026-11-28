package confine

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// RenameNoReplace moves from to to, both in root, and refuses with fs.ErrExist when to is there,
// so nothing is ever replaced. It refuses, with an error matching ErrOutside, a link or junction
// at any component of either name, the last included, a name that is not clean and local or is
// the root, and a file with a second name, a hard link: a hard link to a file elsewhere in the
// root is that file, and no walk sees it. On Windows it refuses, in the same way and before any
// call, a name with a component that ends in a dot or a space.
//
// On Linux it is renameat2 with RENAME_NOREPLACE, and on macOS renameatx_np with RENAME_EXCL,
// from folders opened with no link followed, so the refusal of a taken name is atomic; a kernel
// or file system that does not take the flag refuses with an error matching
// errors.ErrUnsupported. On Windows it moves by handle, from folders it holds open so none can be
// swapped for a junction, in one call that refuses a taken name. FreeBSD and the other systems
// refuse with an error matching errors.ErrUnsupported. A refusal moves nothing.
func RenameNoReplace(root *os.Root, from, to string) error {
	return renameIn(root, from, to, false)
}

// RenameExchange swaps a and b in root, both of which must be there, under RenameNoReplace's
// refusals. On Linux it is renameat2 with RENAME_EXCHANGE, and on macOS renameatx_np with
// RENAME_SWAP. Where the system or the file system has no such swap, Windows and FreeBSD among
// them, it refuses with ErrNoExchange, which matches errors.ErrUnsupported, and nothing moves.
func RenameExchange(root *os.Root, a, b string) error {
	return renameIn(root, a, b, true)
}

func renameIn(root *os.Root, from, to string, exchange bool) error {
	for _, name := range []string{from, to} {
		if err := refusedName(opRename, name); err != nil {
			return err
		}
	}
	if err := rootOnPipes(root, from); err != nil {
		return err
	}
	if exchange && !swaps {
		return &fs.PathError{Op: opRename, Path: filepath.ToSlash(from), Err: fmt.Errorf("this system has no such call: %w", ErrNoExchange)}
	}
	return rename(root, from, to, exchange)
}

// refusedName refuses name, matching ErrOutside, as an act of op on it refuses it: one that is not
// clean and local, the root itself, and on Windows one with a component that ends in a dot or a
// space.
func refusedName(op, name string) error {
	if err := clean(name); err != nil || name == "." {
		return &fs.PathError{Op: op, Path: filepath.ToSlash(name), Err: fmt.Errorf("it is not a clean name below the root: %w", ErrOutside)}
	}
	if stripsTrail && trailed(name, string(filepath.Separator)) {
		return &fs.PathError{Op: op, Path: filepath.ToSlash(name), Err: fmt.Errorf("a component of it ends in a dot or a space, which the Win32 name rules strip, so it would name one file to os.Root and another to the call: %w", ErrOutside)}
	}
	return nil
}

// trailed is whether a component of name ends in a dot or a space: os.Root's Win32 opens strip
// both, the NT calls of the Windows move do not, and filepath.IsLocal takes such a name.
func trailed(name, sep string) bool {
	return slices.ContainsFunc(strings.Split(name, sep), func(p string) bool { return strings.HasSuffix(p, ".") || strings.HasSuffix(p, " ") })
}

package confine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// bySpelling is true: the system takes a .. in a relative link target by its spelling, against
// the link's own folder, before it walks the names.
const bySpelling = true

// readLink answers the target of the link or junction at next and whether the walk follows it. A
// mount point of a whole volume, where the mount manager lists next among the volume's paths, is
// kept as a folder. It refuses, matching ErrOutside: another reparse point that Go reports as
// irregular, which os.Readlink does not read; a target with a name that ends in a dot or a space;
// a junction's target that is not absolute, as no junction's is but one the walk cannot read; and
// a \\.\ target, or a \\?\ one that is not such a mount point.
func readLink(next string, fi fs.FileInfo) (string, bool, error) {
	junction := fi.Mode()&fs.ModeSymlink == 0
	target, err := os.Readlink(next)
	if err != nil {
		if junction {
			return "", false, otherReparse(opResolve, next, err)
		}
		return "", false, err
	}
	if trailedTarget(target) {
		return "", false, &fs.PathError{Op: opResolve, Path: next, Err: fmt.Errorf("its target %q has a name that ends in a dot or a space, which the walk's look by the Win32 rules strips and the link keeps: %w", target, ErrOutside)}
	}
	follow, err := farTarget(next, target)
	if err == nil && follow && junction && !filepath.IsAbs(target) {
		return "", false, &fs.PathError{Op: opResolve, Path: next, Err: fmt.Errorf("it is a junction whose target %q is not an absolute path, so it names no folder the walk can read: %w", target, ErrOutside)}
	}
	return target, follow, err
}

// trailedTarget is whether a name of target, but . and .., ends in a dot or a space.
func trailedTarget(target string) bool {
	return slices.ContainsFunc(names(target), func(n string) bool {
		return n != "." && n != ".." && (strings.HasSuffix(n, ".") || strings.HasSuffix(n, " "))
	})
}

// farTarget is whether the walk follows target, read from the link at next: a target past \\?\ is
// kept as a folder when it is a whole volume's own root, Volume{...}\, and the mount manager lists
// next among that volume's paths, as it does for a mount point, and refused otherwise, as no path
// through a drive answers it; a junction made by hand to a volume's name is not a mount point. A
// \\.\ target is refused: the system follows no such target.
func farTarget(next, target string) (bool, error) {
	if strings.HasPrefix(target, `\\.\`) {
		return false, &fs.PathError{Op: opResolve, Path: next, Err: fmt.Errorf("it leads to %q, a device name the system follows no link to: %w", target, ErrOutside)}
	}
	v, ok := strings.CutPrefix(target, `\\?\`)
	if !ok {
		return true, nil
	}
	if !wholeVolume(v) {
		return false, &fs.PathError{Op: opResolve, Path: next, Err: fmt.Errorf("it leads to %q, a name with no drive letter that no path through a drive can answer: %w", target, ErrOutside)}
	}
	if err := mountedAt(target, next+`\`); err != nil {
		return false, &fs.PathError{Op: opResolve, Path: next, Err: fmt.Errorf("it leads to the volume %q, which is not mounted there (%s): %w", target, err.Error(), ErrOutside)}
	}
	return false, nil
}

// wholeVolume is whether v, a mount point's target past \\?\, is a volume's own root, Volume{...}\.
func wholeVolume(v string) bool {
	head, rest, ok := strings.Cut(v, `\`)
	return ok && rest == "" && len(head) > len("Volume{}") && strings.EqualFold(head[:len("Volume{")], "Volume{") && strings.HasSuffix(head, "}")
}

// asTaken is path as the system takes it before it opens anything, filepath.Clean's answer: a ..
// takes off the name before it by its spelling, whatever that name is, there or not. A device path
// (\\?\, \??\, \\.\) is taken only clean, and an unclean one is refused,
// matching fs.ErrInvalid and ErrOutside: past \\?\ and \??\ the system hands each name to the file
// system as written, with no .. taken, and past \\.\ it takes the first name for one Clean keeps
// as part of the volume, so \\.\UNC\host\share\..\other is \\host\other to it.
func asTaken(path string) (string, error) {
	if devicePath(path) && filepath.Clean(path) != path {
		return "", &fs.PathError{Op: opResolve, Path: path, Err: fmt.Errorf("it is a device path that is not clean, and the system takes no . or .. in one by its spelling: %w", errors.Join(fs.ErrInvalid, ErrOutside))}
	}
	return filepath.Clean(path), nil
}

// devicePath is whether path begins \\?\, \??\ or \\.\, a separator either slash.
func devicePath(path string) bool {
	if len(path) < 4 || !os.IsPathSeparator(path[0]) || !os.IsPathSeparator(path[3]) {
		return false
	}
	return os.IsPathSeparator(path[1]) && (path[2] == '?' || path[2] == '.') || path[1] == '?' && path[2] == '?'
}

// cleanAbs refuses an absolute target with a . or a .. among its names: the system opens such a
// name as it is written, with no .. taken, and the walk would take it.
func cleanAbs(next, target string) error {
	if slices.ContainsFunc(names(target[len(filepath.VolumeName(target)):]), func(n string) bool { return n == "." || n == ".." }) {
		return &fs.PathError{Op: opResolve, Path: next, Err: fmt.Errorf("its target %q has a . or a .. among its names: %w", target, ErrOutside)}
	}
	return nil
}

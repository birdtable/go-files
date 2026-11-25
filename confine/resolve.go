package confine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

const opResolve = "resolve"

// errNotDir is the system's own answer for a name walked through that is not a folder.
var errNotDir error = syscall.ENOTDIR

// maxHops is how many links Resolve follows on one path before it refuses it as a loop: macOS's
// and FreeBSD's own limit, the lowest of the systems' (Linux's is 40, Windows's 63), so no chain
// the system refuses is answered.
const maxHops = 32

// ErrLoop is Resolve's and Below's refusal of a path that passes more than 32 links, as a loop of
// links does. It matches ErrOutside.
var ErrLoop = fmt.Errorf("the path passes more than %d links, as a loop of links does: %w", maxHops, ErrOutside)

// Resolve answers path with every link on it followed, one hop at a time, as the system follows
// them: a symbolic link on every system, and on Windows a junction too, which
// filepath.EvalSymlinks has not followed since Go 1.23. No ownership is judged. path is absolute,
// clean or not, and is taken where the system takes it: on Linux, macOS and the other Unix systems
// it is walked as written, so tree/d/../x, with d a link, is the x beside d's target, and a . or an
// empty name is stepped over; on Windows, which takes a .. by its spelling before it opens
// anything, it is walked as filepath.Clean gives it, so tree\junc\..\x is tree\x, whatever junc
// is. A device path there (\\?\, \??\, \\.\) is taken only clean, as the system takes its names as
// written, and an unclean one is refused, matching fs.ErrInvalid and ErrOutside. A path the system
// itself refuses, as a trailing separator after a file, is refused with the system's own error by
// the look below.
//
// Each name is looked at with os.Lstat below the part already walked, which has no link left in
// it. A link's target is read with os.Readlink and put in front of the names still to walk, so a
// link among the target's own names is followed too, and a relative target is taken against the
// link's own folder. No path is cleaned before its links are read: on Linux, macOS and the other
// Unix systems a .. in a target goes up from the folder the walk has reached, so d/../x, with d a
// link, is the folder above d's target, then x. On Windows the system takes a .. in a relative
// target by its spelling, against the link's folder, and so does Resolve. A target rooted with no
// drive (\x) is on the link's own volume. On Windows a mount point that names a whole volume
// (\\?\Volume{...}\) is kept as a folder, as the system's path through it is, only where the mount
// manager lists the mount point among that volume's paths; the volume has no other name to answer.
//
// Names below the last one that exists are kept as written, so a link to a missing file answers
// where it leads. A .. among those names is refused with fs.ErrNotExist, as the system cannot walk
// up from a folder that is not there, and a name before the last that is not a folder is an
// *fs.PathError holding syscall.ENOTDIR.
//
// Once the walk ends, the system's own os.Stat of path must find the file os.Stat finds at the
// answer, or the path is refused as changed, with ErrChanged, which matches ErrOutside: where the
// answer exists, a link swapped during the walk, or a system that follows a link otherwise than
// the walk, is never answered. Where the answer has a missing name, the system's os.Stat of path
// must find nothing too, or it is refused with ErrChanged, a name made there by another process
// while the path was walked among them, and that is all that is checked: the missing tail is
// where the walk leads, not where the system would make a file, and the two differ for a link the walk reads otherwise than the
// system follows it, as a /proc link of a process in another mount namespace, or on Windows a
// symbolic link to an NT name with no drive (\Device\...).
//
// It refuses, with an *fs.PathError matching ErrOutside: a path that is not absolute (fs.ErrInvalid
// too), more than 32 links (ErrLoop), and on Windows a target on a drive with no
// root (C:x), which leads to the drive's working folder, an absolute target with a . or a ..
// among its names, a target with a name that ends in a dot or a space, which the system strips, a
// junction whose target is not absolute, a device path (\\.\), a target past \\?\ that is not a
// whole volume or names one not mounted at the link, and a reparse point that is neither a link
// nor a junction, never matching fs.ErrNotExist. The answer is a name read now: a link changed
// after the answer is not seen. To act on a file inside a folder, open it with Open.
func Resolve(path string) (string, error) {
	done, rest, err := resolveChecked(path, func() {})
	if err != nil {
		return "", err
	}
	return joinNames(done, rest), nil
}

// Below answers the name of path in folder, in this system's form, as a root takes it, and "." for
// folder itself. path is resolved as Resolve resolves it, then each folder of the answer, from the
// last up to the top of its volume, is compared with folder by file ID, as os.SameFile compares
// them, never by spelling: an 8.3 short name, another case on a volume that folds case, and a
// link to folder all name folder. The name answered is spelled as the resolved path spells it
// below the folder that matched. A path not in folder is refused with an *fs.PathError matching
// ErrOutside, and so is every refusal of Resolve's. path is absolute, clean or not, as Resolve takes
// it. folder is absolute and clean, and a folder now.
func Below(folder, path string) (string, error) {
	if err := absClean(folder); err != nil {
		return "", err
	}
	want, err := os.Stat(folder)
	if err != nil {
		return "", err
	}
	if !want.IsDir() {
		return "", &fs.PathError{Op: opResolve, Path: folder, Err: fmt.Errorf("it is not a folder: %w", fs.ErrInvalid)}
	}
	if err := indexAtPath(folder, want, folder, movedResolve(folder)); err != nil {
		return "", err
	}
	done, rest, err := resolveChecked(path, func() {})
	if err != nil {
		return "", err
	}
	top := topOf(done)
	for p := done; ; p = up(p, top) {
		got, err := os.Stat(p)
		if err != nil {
			return "", err
		}
		if err := indexAtPath(p, got, p, movedResolve(path)); err != nil {
			return "", err
		}
		if os.SameFile(want, got) {
			name := joinNames(strings.TrimLeft(done[len(p):], string(filepath.Separator)), rest)
			if name == "" {
				return ".", nil
			}
			return name, nil
		}
		if p == top {
			return "", &fs.PathError{Op: opResolve, Path: path, Err: fmt.Errorf("it is not in %s: %w", folder, ErrOutside)}
		}
	}
}

// resolveChecked is resolve, then the system's own look at path held against the answer. walked
// runs between the two, so a test of this package swaps a link there. Where the walk found nothing
// at its end, any answer of the system's but fs.ErrNotExist is refused as changed.
func resolveChecked(path string, walked func()) (string, []string, error) {
	done, rest, err := resolve(path)
	if err != nil {
		return "", nil, err
	}
	walked()
	changed := &fs.PathError{Op: opResolve, Path: path, Err: fmt.Errorf("%w: the system's own look at it found another file than the walk", ErrChanged)}
	sys, sysErr := os.Stat(path)
	if len(rest) > 0 {
		if sysErr == nil {
			return "", nil, changed
		}
		if !errors.Is(sysErr, fs.ErrNotExist) {
			return "", nil, &fs.PathError{Op: opResolve, Path: path, Err: fmt.Errorf("%w: the walk found nothing at its end and the system's own look failed otherwise, as on Windows a file link to what is now a folder fails: %w", ErrChanged, sysErr)}
		}
		return done, rest, nil
	}
	got, err := os.Stat(done)
	if errors.Is(sysErr, fs.ErrNotExist) || errors.Is(err, fs.ErrNotExist) {
		return "", nil, changed
	}
	if sysErr != nil || err != nil {
		return "", nil, errors.Join(sysErr, err)
	}
	if !os.SameFile(sys, got) {
		return "", nil, changed
	}
	if err := indexAtPath(path, sys, path, changed); err != nil {
		return "", nil, err
	}
	if err := indexAtPath(done, got, done, changed); err != nil {
		return "", nil, err
	}
	return done, nil, nil
}

// resolve walks path one name at a time and answers the part that exists, with no link left in
// it, and the names below it that do not exist, in order.
// movedResolve is a refusal when a folder on the walk is not the one looked at when its file index is
// read, so a link changed under the walk between the look and the read; it matches ErrChanged.
func movedResolve(path string) error {
	return &fs.PathError{Op: opResolve, Path: path, Err: fmt.Errorf("%w: it was another file when its file index was read", ErrChanged)}
}

// absTaken is path as the system takes it, asTaken's answer, refused unless absolute.
func absTaken(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", &fs.PathError{Op: opResolve, Path: path, Err: fmt.Errorf("it is not an absolute path: %w", errors.Join(fs.ErrInvalid, ErrOutside))}
	}
	return asTaken(path)
}

func resolve(path string) (string, []string, error) {
	taken, err := absTaken(path)
	if err != nil {
		return "", nil, err
	}
	done := topOf(taken)
	todo := names(belowTop(taken, done))
	hops := 0
	for len(todo) > 0 {
		name := todo[0]
		todo = todo[1:]
		if name == "." || name == ".." {
			done = dots(done, name)
			continue
		}
		next := child(done, name)
		fi, err := os.Lstat(next)
		if errors.Is(err, fs.ErrNotExist) {
			return notThere(done, next, append([]string{name}, todo...))
		}
		if err != nil {
			return "", nil, err
		}
		if fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) == 0 {
			if err := throughFile(next, fi, todo); err != nil {
				return "", nil, err
			}
			done = next
			continue
		}
		target, follow, err := readLink(next, fi)
		switch {
		case err != nil:
			return "", nil, err
		case !follow:
			done = next
			continue
		}
		if hops++; hops > maxHops {
			return "", nil, &fs.PathError{Op: opResolve, Path: path, Err: ErrLoop}
		}
		if done, todo, err = hop(done, next, target, todo); err != nil {
			return "", nil, err
		}
	}
	return done, nil, nil
}

// dots is where the walk is after name, a . or a .., in the folder done.
func dots(done, name string) string {
	if name == ".." {
		return up(done, topOf(done))
	}
	return done
}

// notThere answers done and rest, the names from the first one not at next on, with each . left
// out. A .. among them is refused with fs.ErrNotExist, as the system walks up from no folder that
// is not there.
func notThere(done, next string, rest []string) (string, []string, error) {
	rest = slices.DeleteFunc(rest, func(n string) bool { return n == "." })
	if slices.Contains(rest, "..") {
		return "", nil, &fs.PathError{Op: opResolve, Path: next, Err: fmt.Errorf("a .. follows a name that is not there, and the system walks up from no missing folder: %w", fs.ErrNotExist)}
	}
	return done, rest, nil
}

// hop answers where the walk goes on from the link at next, in the folder done, with target read
// from it: the folder the walk starts again from, and the names to walk from there.
func hop(done, next, target string, todo []string) (string, []string, error) {
	vol := filepath.VolumeName(target)
	switch {
	case vol != "" && !filepath.IsAbs(target):
		return "", nil, &fs.PathError{Op: opResolve, Path: next, Err: fmt.Errorf("its target %q is on a drive with no root, which leads to that drive's working folder: %w", target, ErrOutside)}
	case filepath.IsAbs(target):
		top := topOf(target)
		if err := cleanAbs(next, target); err != nil {
			return "", nil, err
		}
		return top, append(names(belowTop(target, top)), todo...), nil
	case target != "" && os.IsPathSeparator(target[0]): // rooted, with no volume, on Windows
		done = topOf(done)
		target = target[1:]
	}
	if bySpelling {
		full := filepath.Join(done, target)
		top := topOf(full)
		return top, append(names(belowTop(full, top)), todo...), nil
	}
	return done, append(names(target), todo...), nil
}

// absClean refuses path unless it is absolute and clean.
func absClean(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return &fs.PathError{Op: opResolve, Path: path, Err: fmt.Errorf("it is not an absolute, clean path: %w", errors.Join(fs.ErrInvalid, ErrOutside))}
	}
	return nil
}

// topOf is the top of path's volume: its volume name and one separator, "/" on Unix.
func topOf(path string) string {
	return filepath.VolumeName(path) + string(filepath.Separator)
}

// names cuts s into its names at every separator, leaving out empty ones.
func names(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == filepath.Separator })
}

// child is name in the folder done.
func child(done, name string) string {
	if strings.HasSuffix(done, string(filepath.Separator)) {
		return done + name
	}
	return done + string(filepath.Separator) + name
}

// up is the folder above done, or top at the top, as the system goes up from it.
func up(done, top string) string {
	if done == top {
		return top
	}
	i := strings.LastIndexByte(done, filepath.Separator)
	if i < len(top) {
		return top
	}
	return done[:i]
}

// joinNames is done with rest below it.
func joinNames(done string, rest []string) string {
	for _, n := range rest {
		if done == "" {
			done = n
			continue
		}
		done = child(done, n)
	}
	return done
}

// belowTop is what of p lies below top, the top of its volume.
func belowTop(p, top string) string { return p[min(len(top), len(p)):] }

// throughFile refuses next, a name that is not a link, when it is not a folder and names are
// still to walk below it, as the system does with ENOTDIR.
func throughFile(next string, fi fs.FileInfo, todo []string) error {
	if !fi.IsDir() && len(todo) > 0 {
		return &fs.PathError{Op: opResolve, Path: next, Err: errNotDir}
	}
	return nil
}

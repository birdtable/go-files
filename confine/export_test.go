package confine

import (
	"io"
	"io/fs"
	"os"
	"testing"
)

// AbsoluteLinks is absoluteLinks, whether a link CopyTree makes holds an absolute target, for the
// tests of package confine_test. It is built into the test binary only.
const AbsoluteLinks = absoluteLinks

// PlantLink is plantLink, for the tests of package confine_test. It is built into the test binary
// only.
func PlantLink(t *testing.T, target, link string) {
	t.Helper()
	plantLink(t, target, link)
}

// WalkOpen is Open by the walk on every system, for the race tests of package confine_test.
func WalkOpen(root *os.Root, name string, flag int, perm fs.FileMode) (*os.File, error) {
	return walkOpen(root, name, flag, perm, func(string) {})
}

// ResolveWalked is Resolve with walked run between the walk and the system's own look at the path,
// so a test swaps a link there. It is built into the test binary only.
func ResolveWalked(path string, walked func()) (string, error) {
	done, rest, err := resolveChecked(path, walked)
	if err != nil {
		return "", err
	}
	return joinNames(done, rest), nil
}

// ResolveWalk is Resolve's walk alone, with no look of the system's held against it, so a test
// shows a refusal of the walk's own and not the check's. It is built into the test binary only.
func ResolveWalk(path string) (string, error) {
	done, rest, err := resolve(path)
	if err != nil {
		return "", err
	}
	return joinNames(done, rest), nil
}

// CollideMade is Collide with made run once the probe a is made, before b is looked at. It is
// built into the test binary only.
func CollideMade(root *os.Root, a, b string, made func()) (bool, error) {
	return collide(root, a, b, made, func(string) {})
}

// CollideLooked is Collide with looked run after each look it makes at a or b, told which: "a"
// and "b" for the first looks, "a again" and "b again" for the second before an answer of apart.
// It is built into the test binary only.
func CollideLooked(root *os.Root, a, b string, looked func(step string)) (bool, error) {
	return collide(root, a, b, func() {}, looked)
}

// NearestLooked is Nearest with look in place of os.Lstat, so a test walks a path no system holds,
// as a share's root with no share served. It is built into the test binary only.
func NearestLooked(start, marker, top string, look func(string) (fs.FileInfo, error)) (string, error) {
	return nearest(start, marker, top, look)
}

// Closing is the close IsRunnable makes after its answer, for a test of a close that fails. It is
// built into the test binary only.
func Closing(c io.Closer, ok bool, err error) (bool, error) {
	return answerClosed(c, ok, err)
}

// IsRunnableBeforeOpen is IsRunnable with before run just before the open, so a test swaps the
// name there. It is built into the test binary only.
func IsRunnableBeforeOpen(root *os.Root, name string, before func()) (bool, error) {
	return isRunnable(root, name, func(step string) {
		if step == stepOpen {
			before()
		}
	})
}

// ClosingCount is the close Names makes after its count, for a test of a close that fails. It is
// built into the test binary only.
func ClosingCount(c io.Closer, n int, err error) (int, error) {
	return answerClosed(c, n, err)
}

// CountOf is the count every system's names are answered by, for a test of a count no volume here
// makes. It is built into the test binary only.
func CountOf(n uint64) int { return countOf(n) }

// NamesBeforeOpen is Names with before run just before the open, so a test swaps the name there.
// It is built into the test binary only.
func NamesBeforeOpen(root *os.Root, name string, before func()) (int, error) {
	return namesAt(root, name, func(step string) {
		if step == stepOpen {
			before()
		}
	})
}

// ReadCappedBeforeOpen is ReadCapped with before run just before the open, so a test swaps the
// name there. It is built into the test binary only.
func ReadCappedBeforeOpen(root *os.Root, name string, limit int64, before func()) ([]byte, error) {
	return readCapped(root, name, limit, func(step string) {
		if step == stepOpen {
			before()
		}
	})
}

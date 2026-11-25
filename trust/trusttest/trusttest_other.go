//go:build !windows

package trusttest

import (
	"io/fs"
	"os"
	"syscall"
)

// special is the mode bits beside the nine that os.Chmod sets, which a change keeps as they are.
const special = fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky

// bit is the one mode bit that lets who in.
func bit(who Who) fs.FileMode {
	switch who {
	case GroupReads:
		return 0o040
	case GroupWrites:
		return 0o020
	case OthersRead:
		return 0o004
	default:
		return 0o002
	}
}

// loosen adds who's bit to path's mode, and is the put that sets the mode it had.
func loosen(path string, who Who) (func() error, error) {
	old, err := modeOf(path)
	if err != nil {
		return nil, err
	}
	return chmod(path, old, old|bit(who))
}

// deny clears path's nine permission bits, and is the put that sets the mode it had.
func deny(path string) (func() error, error) {
	old, err := modeOf(path)
	if err != nil {
		return nil, err
	}
	return chmod(path, old, old&^fs.ModePerm)
}

// modeOf is the mode bits of path that os.Chmod sets.
func modeOf(path string) (fs.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Mode() & (fs.ModePerm | special), nil
}

// chmod gives path the mode to, and is the put that gives it back old.
func chmod(path string, old, to fs.FileMode) (func() error, error) {
	if err := os.Chmod(path, to); err != nil {
		return nil, err
	}
	return func() error { return os.Chmod(path, old) }, nil
}

// samePath is whether real, path with its links resolved, names path: the very same bytes.
func samePath(real, path string) bool { return real == path }

// openWrite opens path to write, without waiting for a pipe's other end, and closes it at once,
// writing nothing.
func openWrite(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0) //nolint:gosec // G304: a candidate of SystemFile's, never written
	if err != nil {
		return err
	}
	return f.Close()
}

// kept is the mode os.Chmod leaves a name given want: want itself.
func kept(want fs.FileMode, _ bool) fs.FileMode { return want }

// openRead opens path to read with O_NONBLOCK, so a pipe at path with no writer opens at once and
// the test goes on, where os.Open would wait for a writer until the test's timeout.
func openRead(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0) //nolint:gosec // G304: the path the caller names
}

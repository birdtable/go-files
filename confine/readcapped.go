package confine

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
)

const opRead = "read"

// ErrNotRegular is ReadCapped's refusal of a name that opened as anything but a regular file: a
// folder, a pipe or a device. Nothing was read from it. A socket is not opened at all, so its
// refusal is the open's own error.
var ErrNotRegular = errors.New("the name is not a regular file, and nothing was read from it")

// ErrOverCap is ReadCapped's refusal of a file that holds more bytes than the cap, told by one
// byte read past it. None of its bytes are answered.
var ErrOverCap = errors.New("the file holds more bytes than the cap, and none are answered")

// ReadCapped reads the file name in root and answers its bytes, opened as Open opens it, read-only,
// with no link followed at any component, and read from that handle alone. name is
// clean and local, as Local gives it.
//
// It refuses, each with an *fs.PathError naming name: a link or junction at any component, the last
// included, and a name that is not clean and local, matching ErrOutside, as Open refuses them; what
// opened as anything but a regular file, by the handle's Stat, matching ErrNotRegular, so a name
// swapped for a folder or a pipe after any look of the caller's is refused, never read; and a file
// longer than limit bytes, matching ErrOverCap, which reading limit+1 bytes tells, so no more than
// that is ever read. A file of exactly limit bytes is answered whole. A limit below zero matches
// fs.ErrInvalid, and nothing is opened. The open never waits, so a pipe at the name is refused at
// once. A socket at the name is refused by the open, with the system's own error and not
// ErrNotRegular, and nothing is read.
//
// The cap is on what is read, not on what Stat says: a file that grows while it is read is refused
// once it passes limit, and one that shrinks is answered as read. A file with a second name, by a
// hard link, is read as any other: a hard link to a file elsewhere is that file, and no walk sees
// it, so Names is the caller's to ask where that matters.
func ReadCapped(root *os.Root, name string, limit int64) ([]byte, error) {
	return readCapped(root, name, limit, func(string) {})
}

// readCapped is ReadCapped with reached run at each step where another process could change the
// path, for a test.
func readCapped(root *os.Root, name string, limit int64, reached func(step string)) ([]byte, error) {
	if limit < 0 {
		return nil, &fs.PathError{Op: opRead, Path: name, Err: fmt.Errorf("a cap of %d bytes: %w", limit, fs.ErrInvalid)}
	}
	if err := clean(name); err != nil {
		return nil, err
	}
	f, err := open(root, name, os.O_RDONLY|noWait, 0, reached)
	if err != nil {
		return nil, err
	}
	b, err := readHandle(f, name, limit)
	return answerClosed(f, b, err)
}

// readHandle reads f, opened at name, as ReadCapped answers it: a regular file of at most limit
// bytes, read through limit+1.
func readHandle(f *os.File, name string, limit int64) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, &fs.PathError{Op: opRead, Path: name, Err: fmt.Errorf("%s: %w", info.Mode().Type(), ErrNotRegular)}
	}
	return readUpTo(f, name, limit)
}

// readUpTo reads r, the file at name, through limit+1 bytes, and answers what it read, or ErrOverCap
// when that is more than limit.
func readUpTo(r io.Reader, name string, limit int64) ([]byte, error) {
	past := limit
	if past < math.MaxInt64 {
		past++
	}
	b, err := io.ReadAll(io.LimitReader(r, past))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, &fs.PathError{Op: opRead, Path: name, Err: fmt.Errorf("more than %d bytes: %w", limit, ErrOverCap)}
	}
	return b, nil
}

package confine

import (
	"fmt"
	"io/fs"
	"math"
	"os"
)

const opNames = "names"

// Names answers how many names the file name in root has, by hard links, from the file itself,
// opened as Open opens it, read-only and never waiting, then counted by NamesFile, and from
// nothing outside the root. name is clean and local, as Local gives it. A link or
// junction at any component, the last included, is refused with an *fs.PathError matching
// ErrOutside, as Open refuses it, whether it leads outside the root or stays inside it; a name
// swapped for one during the call is refused or counted as the file opened, never read outside.
// An error is no count, and a guard refuses on it: a file this account may write but not read,
// which the read-only open refuses, included.
func Names(root *os.Root, name string) (int, error) {
	return namesAt(root, name, func(string) {})
}

// NamesFile answers how many names the open file f has, as its volume reports them: st_nlink on
// Unix, NumberOfLinks on Windows, read from f. Anything but a regular file is
// refused with an *fs.PathError matching fs.ErrInvalid, and a count the system will not give is
// its error, never 1. A count past 2^31-1 is answered as 2^31-1. 0 is an answer, for a file whose
// every name was removed while it was held, so a guard compares with 1. A volume that cannot hold a
// second name, as FAT, reports 1, and a network volume what its server sends. A name made after the
// count is not seen, so a guard holds the handle it counted through to its act.
func NamesFile(f *os.File) (int, error) {
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, &fs.PathError{Op: opNames, Path: f.Name(), Err: fmt.Errorf("it is %v, and only a regular file's names are counted: %w", info.Mode().Type(), fs.ErrInvalid)}
	}
	n, err := linkCount(f, info)
	if err != nil {
		return 0, &fs.PathError{Op: opNames, Path: f.Name(), Err: err}
	}
	return n, nil
}

// namesAt is Names with reached run at each step where another process could change the path, for
// a test.
func namesAt(root *os.Root, name string, reached func(step string)) (int, error) {
	if err := clean(name); err != nil {
		return 0, err
	}
	f, err := open(root, name, os.O_RDONLY|noWait, 0, reached)
	if err != nil {
		return 0, err
	}
	n, err := NamesFile(f)
	return answerClosed(f, n, err)
}

// countOf is n as an int on every build: a count past math.MaxInt32 is answered as that, never wrapped.
func countOf(n uint64) int {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	return int(n)
}

// shared is whether f, whatever file it is, has a second name.
func shared(f *os.File) (bool, error) {
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	n, err := linkCount(f, info)
	return n > 1, err
}

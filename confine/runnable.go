package confine

import (
	"errors"
	"io"
	"os"
)

// IsRunnable answers whether this system would run the file name in root for this account, read
// from the file itself, opened as Open opens it, read-only, then answered by IsRunnableFile, and
// from nothing outside the root. name is clean and local, as Local gives it.
//
// A link or junction at any component, the last included, is refused with false and an
// *fs.PathError matching ErrOutside, as Open refuses it: a file read through a link is not
// answered confined. The answer comes from the open file alone, so a name swapped for a link
// during the call is refused or answered as the file opened, never read outside. The open never
// waits, so a pipe or a device at the name is answered false at once. An open that fails is false
// with its error.
//
// A file this account may run but not read, mode 0111 or a Windows list that grants execute
// alone, is refused by the read-only open, and answered false with that error, not guessed.
func IsRunnable(root *os.Root, name string) (bool, error) {
	return isRunnable(root, name, func(string) {})
}

// IsRunnableFile answers whether this system would run the open file f for this account, and
// nothing else: for whatever file f is, so how f was opened, and whether it stayed inside a root,
// is the caller's business. Anything but a regular file is false, with no error.
//
// On Unix the exec bit is the one that applies to this account, by the open file's fstat: the
// owner's if this account owns the file, else the group's if the file's group is one of this
// account's, primary or supplementary, else other's; root, euid 0, needs any one of the three.
// Access lists, mount options and capabilities are not read: a 0755 file on a noexec mount is
// answered true, and so may a file be for a root that lacks CAP_DAC_OVERRIDE over it, a user
// namespace's root on a file whose owner is not mapped into it or a root with the capability
// dropped, where running it is refused.
//
// On Windows the file's name, as the handle names it, its long name whatever the caller spelled,
// must end in an extension PATHEXT lists, compared without case, or in .com, .exe, .bat or .cmd,
// Go's own list, when PATHEXT is unset; a name with no extension is false. That is stricter than
// exec.LookPath on an absolute name, which asks only for a dot in it, so it answers README.md
// runnable, and runs foo.exe for a name foo with no dot. A file with several names, by hard links,
// is answered by the one the handle gives, one of its names.
func IsRunnableFile(f *os.File) (bool, error) {
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false, err
	}
	return runs(f, info)
}

// isRunnable is IsRunnable with reached run at each step where another process could change the
// path, for a test.
func isRunnable(root *os.Root, name string, reached func(step string)) (bool, error) {
	if err := clean(name); err != nil {
		return false, err
	}
	f, err := open(root, name, os.O_RDONLY|noWait, 0, reached)
	if err != nil {
		return false, err
	}
	ok, err := IsRunnableFile(f)
	return answerClosed(f, ok, err)
}

// answerClosed closes c after the answer v, err was had from it: a close that fails answers v's
// zero, false or 0, with its error joined to err, as every other failure does.
func answerClosed[T any](c io.Closer, v T, err error) (T, error) {
	if cerr := c.Close(); cerr != nil {
		var zero T
		return zero, errors.Join(err, cerr)
	}
	return v, err
}

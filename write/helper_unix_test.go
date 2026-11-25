//go:build !windows

package write

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

// linkFolder makes at, beneath root, a symbolic link to the folder target, as confine.LinkFolder
// makes one on Unix; write's tests do not import confine.
func linkFolder(t *testing.T, root *os.Root, target, at string) error {
	t.Helper()
	return os.Symlink(target, filepath.Join(root.Name(), at))
}

// umaskWrite writes path under the octal umask mask, with Mode(perm), no Mode when perm is "-",
// Shared when it is "shared", and Shared with Runnable when it is "shared-run".
func umaskWrite(mask, path, perm string) error {
	m, err := strconv.ParseUint(mask, 8, 32)
	if err != nil {
		return err
	}
	syscall.Umask(int(m))
	switch perm {
	case "-":
		return File(path, []byte("x"))
	case "shared":
		return File(path, []byte("x"), Shared())
	case "shared-run":
		return File(path, []byte("x"), Shared(), Runnable())
	}
	p, err := strconv.ParseUint(perm, 8, 32)
	if err != nil {
		return err
	}
	return File(path, []byte("x"), Mode(fs.FileMode(p)))
}

// umaskLog appends two lines to the log at path, the second past the cap so the log is moved
// aside and made again, under the octal umask mask, with LogMode(perm), or none when perm is "-".
func umaskLog(mask, path, perm string) error {
	m, err := strconv.ParseUint(mask, 8, 32)
	if err != nil {
		return err
	}
	syscall.Umask(int(m))
	opts := []LogOption{Aside(filepath.Base(path) + ".1")}
	if perm != "-" {
		p, err := strconv.ParseUint(perm, 8, 32)
		if err != nil {
			return err
		}
		opts = append(opts, LogMode(fs.FileMode(p)))
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	for _, line := range []string{"first", "second"} {
		if err := Append(context.Background(), root, filepath.Base(path), []byte(line), 8, opts...); err != nil {
			return errors.Join(err, root.Close())
		}
	}
	return root.Close()
}

// permIs fails the test when the file at path does not have the mode perm.
func permIs(t *testing.T, path string, perm os.FileMode) {
	t.Helper()
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != perm {
		t.Errorf("%s is %v (%v), and want the mode %v", path, fi, err, perm)
	}
}

// keptAs fails t unless the file at path has the mode perm, the mode a copy keeps.
func keptAs(t *testing.T, path string, perm os.FileMode) {
	t.Helper()
	permIs(t, path, perm)
}

// holdShared takes a shared flock on the file at path through a descriptor opened only to read, as
// any reader of the log can, and answers the call that lets go, which the test's end makes if the
// test has not.
func holdShared(t *testing.T, path string) func() {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	letGo := func() { _ = f.Close() }
	t.Cleanup(letGo)
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH); err != nil { //nolint:forbidigo // register (path-lock): a reader's shared hold on Append's log, which the test needs Append to refuse
		t.Fatal(err)
	}
	return letGo
}

// held is the third swap's act on the held log's name as the hard-link test wants it: done, as the
// count of names is read from the look the hold's check matched.
func held(_ string, err error) error { return err }

// taken is a third swap's act whose result the hard-link test judges only on Unix: done, as the
// count of names is read from the look the hold's check matched.
func taken(_ *testing.T, _ string, err error) error { return err }

// replaced is a replace row's act, which the hard-link test judges on Unix as it does taken's: done.
func replaced(_ *testing.T, _ string, err error) error { return err }

// linkOver makes a hard link to the file at from at the name to, over the file there: Unix has no
// link that replaces, so it is a hard link at a name beside to, renamed over it. posix is Windows's.
func linkOver(from, to string, _ bool) error {
	beside := filepath.Join(filepath.Dir(to), "over")
	if err := os.Link(from, beside); err != nil {
		return err
	}
	return os.Rename(beside, to)
}

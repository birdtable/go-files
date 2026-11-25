//go:build unix

package lock

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	noFollow = syscall.O_NOFOLLOW
	noBlock  = syscall.O_NONBLOCK // a pipe put at the note's name is not waited on
)

func unlinked(in, string, *os.File) error { return nil } // noFollow refused a link at the open

// openErr names an open's refusal at a link (ELOOP; EMLINK on FreeBSD) and at
// a socket (ENXIO on Linux, EOPNOTSUPP on macOS).
func openErr(err error) error {
	switch {
	case errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.EMLINK):
		return fmt.Errorf("%w: %w", errLink, err)
	case errors.Is(err, syscall.ENXIO) || errors.Is(err, syscall.EOPNOTSUPP):
		return fmt.Errorf("%w: %w", errNotPlain, err)
	}
	return err
}

func retried(op func() error) error { return op() }

func inUse(error) bool { return false }

// owned gives the note's temp file the lock file's mode, and first its owner when beside, the note
// in the lock file's own folder. Elsewhere the note is this process's: a new file there may take a
// group this account cannot give away, and a holder that runs as root hands no account a file in
// a folder that account could not write.
func owned(tmp *os.File, lockFile os.FileInfo, beside bool) error {
	if st, ok := lockFile.Sys().(*syscall.Stat_t); ok && beside {
		if err := tmp.Chown(int(st.Uid), int(st.Gid)); err != nil {
			return err
		}
	}
	return tmp.Chmod(lockFile.Mode().Perm())
}

// newLock makes the lock file at path, looked up as at says, by an exclusive create that follows no
// link, to read and write, 0600 before openLock sets it whatever the umask. Every volume here keeps
// a mode, so listless is not read.
func newLock(at in, path string, _ bool) (*os.File, error) {
	return at.open(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
}

// open opens name as flag says, with no link followed at its last element: by path, or below at's
// root by openat in name's folder, opened through the root as a folder and never waited on.
func (at in) open(name string, flag int, perm fs.FileMode) (*os.File, error) {
	if at.root == nil {
		return os.OpenFile(name, flag|noFollow, perm) //nolint:gosec // the caller's lock or note path, opened with no link followed
	}
	d, err := at.root.OpenFile(filepath.Dir(name), os.O_RDONLY|unix.O_DIRECTORY|noBlock, 0)
	if err != nil {
		return nil, err
	}
	defer d.Close() //nolint:errcheck // a folder opened only to open in; its close loses nothing
	c, err := d.SyscallConn()
	if err != nil {
		return nil, err
	}
	fd := -1
	if cerr := c.Control(func(dir uintptr) {
		for {
			fd, err = unix.Openat(int(dir), filepath.Base(name), flag|noFollow|unix.O_CLOEXEC, uint32(perm.Perm()))
			if !errors.Is(err, unix.EINTR) {
				return
			}
		}
	}); cerr != nil {
		return nil, cerr
	}
	if err != nil {
		return nil, &fs.PathError{Op: "openat", Path: name, Err: err}
	}
	return os.NewFile(uintptr(fd), name), nil
}

// indexAt refuses nothing off Windows: every Unix volume gives a file an inode os.SameFile can trust,
// so a lock-file compare by file ID is never on a volume with none.
func indexAt(*os.File, string) error { return nil }

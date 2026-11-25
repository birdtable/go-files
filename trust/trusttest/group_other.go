//go:build !windows

package trusttest

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"syscall"
)

// giveGroup gives path to gid by chown, and is the put that gives it back its group and the
// special bits either change cleared.
func giveGroup(path string, gid uint32, chown func(string, int, int) error) (func() error, error) {
	old, err := groupOf(path)
	if err != nil {
		return nil, err
	}
	mode, err := modeOf(path)
	if err != nil {
		return nil, err
	}
	switch back, err := isMine(old); {
	case err != nil:
		return nil, err
	case !back: // the kernel's rule is the same both ways, so it could not be given back
		return nil, fmt.Errorf("%w: its group %d is not one of this account's, and this account is not root, so it could not give it back at the test's end", errCannot, old)
	}
	if err := chown(path, -1, int(gid)); err != nil {
		switch {
		case errors.Is(err, syscall.EPERM):
			return nil, fmt.Errorf("%w: this account may not give it to the group %d, as an account but root may not give a name it does not own, or give one to a group it is not in: %w", errCannot, gid, err)
		case errors.Is(err, syscall.EINVAL):
			return nil, fmt.Errorf("%w: the group %d is not one this user namespace maps: %w", errCannot, gid, err)
		}
		return nil, err
	}
	put := func() error {
		pre, err := modeOf(path)
		if err != nil {
			return err
		}
		if err := chown(path, -1, int(old)); err != nil {
			return err
		}
		now, err := modeOf(path)
		if err != nil {
			return err
		}
		if want := now | (pre|mode)&special; want != now {
			return os.Chmod(path, want)
		}
		return nil
	}
	got, err := groupOf(path)
	if err == nil && got != gid {
		err = fmt.Errorf("%w: its disk kept the group %d, not %d, as a disk that keeps no groups does", errCannot, got, gid)
	}
	if err != nil {
		if perr := put(); perr != nil {
			return nil, fmt.Errorf("%w; putting back what it changed: %w", err, perr)
		}
		return nil, err
	}
	return put, nil
}

// isMine is whether this account may give a name it owns to the group gid: it is root, or gid is
// its effective group or one of os.Getgroups.
func isMine(gid uint32) (bool, error) {
	if os.Geteuid() == 0 || os.Getegid() == int(gid) {
		return true, nil
	}
	groups, err := os.Getgroups()
	if err != nil {
		return false, err
	}
	return slices.Contains(groups, int(gid)), nil
}

// groupOf is the group of path, followed where it is a link.
func groupOf(path string) (uint32, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("its stat is a %T, which holds no group", info.Sys())
	}
	return st.Gid, nil
}

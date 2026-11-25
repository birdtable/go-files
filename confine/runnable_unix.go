//go:build unix

package confine

import (
	"errors"
	"io/fs"
	"os"
	"slices"
	"syscall"
)

// runs is the exec bit of the open file f, of info, that applies to this account.
func runs(_ *os.File, info fs.FileInfo) (bool, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, &fs.PathError{Op: opOpen, Path: info.Name(), Err: errors.New("its stat holds no owner")}
	}
	groups, err := os.Getgroups()
	if err != nil {
		return false, err
	}
	return execBit(info.Mode().Perm(), st.Uid, st.Gid, os.Geteuid(), append(groups, os.Getegid())), nil
}

// execBit is whether perm, of a file owned by uid and the group gid, lets the account me, in
// groups, run it: root by any exec bit, the owner by the owner's, a group's account by the
// group's, any other by other's. uid is compared as the 32-bit word it is, as me is on a 32-bit
// build, where an account at 2^31 or past it has a negative euid.
func execBit(perm fs.FileMode, uid, gid uint32, me int, groups []int) bool {
	switch {
	case me == 0:
		return perm&0o111 != 0
	case uid == uint32(me): //nolint:gosec // G115: a uid is a 32-bit word, and a 32-bit build's negative euid wraps back to it
		return perm&0o100 != 0
	case slices.Contains(groups, int(gid)):
		return perm&0o010 != 0
	}
	return perm&0o001 != 0
}

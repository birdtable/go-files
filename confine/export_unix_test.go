//go:build unix

package confine

import "io/fs"

// ExecBit is execBit, for the tests of package confine_test. It is built into the test binary
// only.
func ExecBit(perm fs.FileMode, uid, gid uint32, me int, groups []int) bool {
	return execBit(perm, uid, gid, me, groups)
}

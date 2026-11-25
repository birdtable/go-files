//go:build !linux

package trusttest

import "testing"

// bind skips t: off Linux no account but root mounts a disk with a flag of its choosing.
func bind(t testing.TB, _ Bound, _ []string, _ hook) (entry, dir string) {
	t.Helper()
	t.Skipf("trusttest.Bind: it mounts on Linux alone")
	return "", ""
}

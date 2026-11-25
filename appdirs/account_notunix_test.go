//go:build !unix

package appdirs

import "testing"

// accountWithoutCgo is what AccountHome answers in a build with the osusergo tag and without cgo:
// on Windows, where os/user's Current reads the token's profile folder in every build, the same as
// this test binary's.
func accountWithoutCgo(t *testing.T) (string, error) {
	t.Helper()
	return AccountHome()
}

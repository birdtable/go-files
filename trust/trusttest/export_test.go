package trusttest

import (
	"os"
	"testing"
)

// BindOver is Bind with entries in place of /mnt and /srv, so a test of package trusttest_test
// reaches the skip where no folder qualifies. It is built into the test binary only.
func BindOver(t testing.TB, how Bound, entries []string) (entry, dir string) {
	t.Helper()
	return bind(t, how, entries, hook{})
}

// BindHooked is BindOver with blind, which drops Bind's checks before its mount, as a Bind's whose
// read came before another's mount, and between, run after its mount and before its check after
// it, so a test of package trusttest_test makes two Binds' mounts meet; between must return, and
// not end its goroutine. It is built into the test binary only.
func BindHooked(t testing.TB, how Bound, entries []string, blind bool, between func()) (entry, dir string) {
	t.Helper()
	return bind(t, how, entries, hook{blind: blind, between: between})
}

// DenyBy is Deny with open in place of its open of the name, so a test of package trusttest_test
// reaches an open that fails for a reason other than the denial, or one that opens. It is built
// into the test binary only.
func DenyBy(t testing.TB, path string, open func(string) (*os.File, error)) {
	t.Helper()
	denyBy(t, path, open)
}

// SystemFileAs is SystemFile over cands, as the account me, learning by write whether me may open
// a candidate to write, so a test of package trusttest_test reaches each reason a candidate does
// not pass, the skip and the failure. It is built into the test binary only.
func SystemFileAs(t testing.TB, cands []string, me string, write func(string) error) (path, owner string) {
	t.Helper()
	return systemFile(t, cands, me, write)
}

// GiveGroupBy is GiveGroup changing the group by chown, so a test of package trusttest_test
// reaches a change refused for each reason, one that fails, one the disk does not keep and a put
// back refused. It is built into the test binary only.
func GiveGroupBy(t testing.TB, path string, gid uint32, chown func(string, int, int) error) {
	t.Helper()
	giveGroupBy(t, path, gid, chown)
}

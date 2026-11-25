package trusttest

import (
	"errors"
	"math"
	"os"
	"testing"
)

// noChange is the gid chown reads as "leave the group as it is", so GiveGroup refuses it.
const noChange = math.MaxUint32

// errCannot is a group this system or this account cannot give a name, which skips the test.
var errCannot = errors.New("this account cannot give it the group here")

// GiveGroup gives path to the group gid until the test ends, when its group is put back as it
// was, as are the set-user-ID and set-group-ID bits a change of group clears on Unix, so a test
// plants a name of this account's given to a group, as the admin group trust.AdminGroup answers,
// with no os.Chown of its own. It follows a link at path, as os.Chown does. The test
// is skipped, saying why, before any change where this account, not root, is not in path's own
// group, so it could not give path back, as on macOS with a name below /tmp, which takes the group
// wheel; where it may not give path to gid, being neither root nor in that group, or not path's
// owner; where a Linux user namespace maps no gid; where the disk keeps another group than gid, as
// one that keeps no groups does; and on Windows, which keeps no group id on a name. A gid of
// math.MaxUint32, which chown reads as no change, fails the test, and so does any other failure,
// a put back refused at the test's end among them, as something changed the name meanwhile.
func GiveGroup(t testing.TB, path string, gid uint32) {
	t.Helper()
	giveGroupBy(t, path, gid, os.Chown)
}

// giveGroupBy is GiveGroup changing the group by chown: os.Chown, or in trusttest's own tests one
// that is refused or does nothing.
func giveGroupBy(t testing.TB, path string, gid uint32, chown func(string, int, int) error) {
	t.Helper()
	if gid == noChange {
		t.Fatalf("trusttest.GiveGroup %s: %d is no group: chown reads it as no change", path, gid)
	}
	put, err := giveGroup(path, gid, chown)
	if errors.Is(err, errCannot) {
		t.Skipf("trusttest.GiveGroup %s: %v", path, err)
	}
	if err != nil {
		t.Fatalf("trusttest.GiveGroup %s: %v", path, err)
	}
	t.Cleanup(putBack(t, "GiveGroup", path, put))
}

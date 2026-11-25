package trusttest_test

import (
	"math"
	"testing"

	"github.com/birdtable/go-files/trust/trusttest"
)

// What it costs when this fails: on Windows, which keeps no group id on a name, a test that gives
// its name to a group passes having planted nothing, or fails for what the system cannot do, not
// for the program.
func TestGiveGroupSkipsOnWindows(t *testing.T) {
	t.Parallel()
	p, _ := private(t, false)
	sd := sddl(t, p)
	r := call(t, func(r *recorder) { trusttest.GiveGroup(r, p, 544) })
	if !r.skipped || r.failed || !r.says("Windows keeps no group id on a name") {
		t.Errorf("skipped %v, failed %v, %q, and want a skip saying Windows keeps no group id", r.skipped, r.failed, r.said)
	}
	if after := sddl(t, p); after != sd {
		t.Errorf("its descriptor is %s, and was %s", after, sd)
	}
	if r := call(t, func(r *recorder) { trusttest.GiveGroup(r, p, math.MaxUint32) }); !r.failed || !r.says("is no group") {
		t.Errorf("of no group: failed %v, %q", r.failed, r.said)
	}
}

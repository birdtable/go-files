package trusttest_test

import (
	"testing"

	"github.com/birdtable/go-files/trust/trusttest"
)

// What it costs when this fails: a Bound Bind does not know mounts with no flag, or the wrong
// one, and a test judges a disk it did not ask for.
func TestBindFailsABoundItDoesNotKnow(t *testing.T) {
	t.Parallel()
	for _, how := range []trusttest.Bound{0, trusttest.NoexecWithoutNosuid + 1} {
		r := call(t, func(r *recorder) { trusttest.Bind(r, how) })
		r.end()
		if !r.failed || !r.says("is no Bound") {
			t.Errorf("Bind of %d failed %v, %q, and want it failed saying it is no Bound", how, r.failed, r.said)
		}
	}
}

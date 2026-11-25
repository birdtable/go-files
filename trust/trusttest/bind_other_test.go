//go:build !linux

package trusttest_test

import (
	"testing"

	"github.com/birdtable/go-files/trust/trusttest"
)

// Off Linux no account but root mounts a disk with a flag of its choosing, so Bind skips the test,
// saying so.
//
// What it costs when this fails: a test of a mount fails on macOS or Windows, where no mount can
// be made, or passes there having judged none.
func TestBindSkipsOffLinux(t *testing.T) {
	t.Parallel()
	for _, how := range []trusttest.Bound{trusttest.Nosuid, trusttest.NoexecWithoutNosuid} {
		r := call(t, func(r *recorder) { trusttest.Bind(r, how) })
		r.end()
		if !r.skipped || r.failed || !r.says("it mounts on Linux alone") {
			t.Errorf("Bind of %d off Linux skipped %v, failed %v, %q, and want it skipped saying why", how, r.skipped, r.failed, r.said)
		}
	}
}

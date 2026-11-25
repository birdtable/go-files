//go:build unix

package trusttest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/trust/trusttest"
)

// What it costs when this fails: a test is handed a name with a link on the way to it, which the
// link's owner may point at a file of its own after the check, and which trust's walk refuses
// whatever it leads to.
func TestSystemFileFailsACandidateWithALinkOnTheWay(t *testing.T) {
	t.Parallel()
	target := tempFile(t, "target")
	dir := filepath.Dir(target)
	if err := os.Symlink(target, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir, filepath.Join(dir, "up")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(dir, "link"), filepath.Join(dir, "up", "target")} {
		r := call(t, func(r *recorder) { trusttest.SystemFileAs(r, []string{p}, other, refused) })
		if !r.failed || !r.says(p+": a link is on the way to it, which leads to "+target) {
			t.Errorf("%s: failed %v: %q, and want it failed saying the link", p, r.failed, r.said)
		}
	}
}

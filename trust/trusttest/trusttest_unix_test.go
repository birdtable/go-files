//go:build unix

package trusttest_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/birdtable/go-files/trust/trusttest"
)

// What it costs when this fails: a change a test made stays made, after the test, with nothing
// said, when it cannot be put back, or a test that removed its name fails for the change it can
// no longer put back.
func TestAChangeThatCannotBePutBackFailsTheTest(t *testing.T) {
	t.Parallel()
	p, mode := private(t, false)
	folder := filepath.Dir(p)
	r := call(t, func(r *recorder) { trusttest.Loosen(r, p, trusttest.OthersRead) })
	if r.failed || r.skipped {
		t.Fatalf("%q", r.said)
	}
	if err := os.Chmod(folder, 0); err != nil {
		t.Fatal(err)
	}
	r.end()
	if err := os.Chmod(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	// Root searches a folder of mode 0, so its put back is made.
	if os.Geteuid() == 0 {
		if r.failed {
			t.Errorf("as root: %q", r.said)
		}
		trusttest.Perm(t, p, mode)
	} else if !r.failed || !r.says("putting back what it changed") {
		t.Errorf("a put back refused passes: failed %v, %q", r.failed, r.said)
	}

	r = call(t, func(r *recorder) { trusttest.Loosen(r, p, trusttest.OthersRead) })
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if r.end(); r.failed {
		t.Errorf("a name the test removed: %q", r.said)
	}
}

// What it costs when this fails: a test that checks the mode of a pipe, or keeps root out of one,
// waits for a writer that never comes, and hangs until its timeout in place of failing or
// skipping.
func TestPermAndDenyOfAPipeReturnAndNeverWait(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o600); err != nil { // past the umask
		t.Fatal(err)
	}
	if r := call(t, func(r *recorder) { trusttest.Perm(r, p, 0o600) }); !r.failed || !r.says("not private") {
		t.Errorf("Perm of a pipe, private by its mode: failed %v, %q, and want a failure saying it is not private", r.failed, r.said)
	}
	r := call(t, func(r *recorder) { trusttest.Deny(r, p) })
	if root := os.Geteuid() == 0; r.failed || r.skipped != root {
		t.Errorf("Deny of a pipe, as root %v: skipped %v, failed %v, %q", root, r.skipped, r.failed, r.said)
	}
	r.end()
}

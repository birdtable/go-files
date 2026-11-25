package trusttest_test

import (
	"os/exec" //nolint:depguard // cli.process.1: the test gives a name a macOS access list entry by chmod +a, as a person does; the module has no runner
	"testing"

	"github.com/birdtable/go-files/trust/trusttest"
)

// What it costs when this fails: on macOS a name whose mode is private, and whose access list lets
// every account read it, passes as private to this account.
func TestPermFailsANameAMacListLetsOthersReadThoughItsModeIsPrivate(t *testing.T) {
	t.Parallel()
	p, mode := private(t, false)
	if out, err := exec.CommandContext(t.Context(), "/bin/chmod", "+a", "everyone allow read", p).CombinedOutput(); err != nil { //nolint:gosec // the test's own name
		t.Fatalf("chmod +a: %v: %s", err, out)
	}
	if r := call(t, func(r *recorder) { trusttest.Perm(r, p, mode) }); !r.failed || !r.says("not private") {
		t.Errorf("a name its list lets everyone read passes as private: %q", r.said)
	}
}

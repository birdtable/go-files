package trusttest_test

import (
	"bufio"
	"fmt"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test starts unshare, with this test binary again in it, as the Bind rows do; the module has no runner
	"slices"
	"strings"
	"testing"

	"github.com/birdtable/go-files/trust/trusttest"
)

// systemNSTest is the test run again in the user namespaces unshare makes, and systemNSArg marks
// that run, before the name of its namespace.
const (
	systemNSTest = "TestSystemFileInAUserNamespaceAnswersTheOverflowAccountAsTheOwner"
	systemNSArg  = "trusttest-system-namespace"
)

// wantSystemOwner is how root, which owns /etc/passwd, reads in this process's user namespace:
// uid:N where its uid_map maps the host's root to N, and the overflow account where it maps none.
func wantSystemOwner(t *testing.T) string {
	t.Helper()
	f, err := os.Open("/proc/self/uid_map")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for s := bufio.NewScanner(f); s.Scan(); {
		var in, out, n uint64
		if _, err := fmt.Sscan(s.Text(), &in, &out, &n); err != nil {
			t.Fatalf("/proc/self/uid_map: the line %q: %v", s.Text(), err)
		}
		if out == 0 && n > 0 {
			return fmt.Sprint("uid:", in)
		}
	}
	return overflowAccount(t)
}

// overflowAccount is the kernel's overflow account, uid:65534 by default, as an owner is spelled.
func overflowAccount(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("/proc/sys/kernel/overflowuid")
	if err != nil {
		t.Fatal(err)
	}
	return "uid:" + strings.TrimSpace(string(b))
}

// What it costs when this fails: in a Linux user namespace that does not map the host's root, as
// go-testkit's contain makes and a rootless container is, a test is handed root's file as uid:0's,
// so a caller that assumes root's spelling passes where the program meets the overflow account;
// or a test that runs as uid 0 in the namespace, where root's file is still kept from it, skips
// where it must pass.
func TestSystemFileInAUserNamespaceAnswersTheOverflowAccountAsTheOwner(t *testing.T) {
	if i := slices.Index(os.Args, systemNSArg); i > 0 && i+1 < len(os.Args) {
		path, owner := trusttest.SystemFile(t)
		if owner == "uid:0" {
			t.Errorf("%s: owned by uid:0, in a namespace that does not map root", path)
		}
		systemFileHolds(t, path, owner, overflowAccount(t))
		t.Logf("the namespace row ran: %s, %s owned by %s", os.Args[i+1], path, owner)
		return
	}
	t.Parallel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// No skip: a runner that cannot unshare fails these rows, as it fails trust's namespace rows.
	for _, row := range []struct{ name, flag string }{{"mapped", "--map-current-user"}, {"root", "--map-root-user"}} {
		out, err := exec.CommandContext(t.Context(), "unshare", "--user", row.flag, exe, "-test.run=^"+systemNSTest+"$", "-test.count=1", "-test.v", systemNSArg, row.name).CombinedOutput() //nolint:gosec // G204: unshare and this test binary, with literal flags
		if err != nil || !strings.Contains(string(out), "the namespace row ran: "+row.name) {
			t.Errorf("this test in unshare --user %s: %v:\n%s", row.flag, err, out)
		}
	}
}

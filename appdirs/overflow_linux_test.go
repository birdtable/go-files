package appdirs

import (
	"errors"
	"fmt"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test starts unshare, with this test binary again in it, as trust's namespace rows do; the module has no runner
	"slices"
	"strings"
	"testing"

	"github.com/birdtable/go-files/trust"
)

// overflowTaken is whether trust takes Who.Overflow on this system at all: Linux alone.
const overflowTaken = true

// uidMapMapsRoot is whether /proc/self/uid_map has a line whose range starts at id 0.
func uidMapMapsRoot(t *testing.T) bool {
	t.Helper()
	b, err := os.ReadFile("/proc/self/uid_map")
	if err != nil {
		t.Fatal(err)
	}
	return slices.ContainsFunc(strings.Split(strings.TrimSpace(string(b)), "\n"), func(l string) bool {
		f := strings.Fields(l)
		return len(f) == 3 && f[0] == "0"
	})
}

// overflowTest is the test run again in a user namespace, and overflowArg marks that run.
const (
	overflowTest = "TestInAUserNamespaceTempOverflowIsAPrivateFolderAndTempRefusesTheOverflowAccount"
	overflowArg  = "appdirs-namespace"
)

// What it costs when this fails: a test in a user namespace that maps this account and not root,
// where / shows as the kernel's overflow account, has no private temp folder of its own with the
// opt-in, or Temp, without it, trusts that account. The rows run in the namespace,
// in the test's own body, so each red line names its own check.
func TestInAUserNamespaceTempOverflowIsAPrivateFolderAndTempRefusesTheOverflowAccount(t *testing.T) {
	if slices.Contains(os.Args, overflowArg) {
		b, err := os.ReadFile("/proc/sys/kernel/overflowuid")
		var uid uint32
		if _, scan := fmt.Sscan(string(b), &uid); err != nil || scan != nil {
			t.Fatalf("the overflow account: %v, %v", err, scan)
		}
		const app = "go-files-temp-overflow"
		plain, err := Temp(app)
		if !errors.Is(err, trust.ErrNotOwners) || !strings.Contains(fmt.Sprint(err), fmt.Sprintf("uid:%d", uid)) {
			t.Errorf("Temp = %q, %v, and want trust.ErrNotOwners naming uid:%d", plain, err, uid)
		}
		got, err := TempOverflow(app)
		if err != nil {
			t.Fatalf("TempOverflow: %v", err)
		}
		if info, err := os.Stat(got); err != nil {
			t.Errorf("TempOverflow = %s: %v", got, err)
		} else if info.Mode().Perm() != 0o700 {
			t.Errorf("TempOverflow = %s, of mode %v, and want 0700", got, info.Mode().Perm())
		}
		if _, err := trust.Path(got, trust.PrivateFolder, trust.Who{NoLinks: true, Overflow: true}); err != nil {
			t.Errorf("trust refuses TempOverflow's answer %s as private: %v", got, err)
		}
		if again, err := TempOverflow(app); err != nil || again != got {
			t.Errorf("TempOverflow again = %s, %v, and want %s", again, err, got)
		}
		t.Logf("the namespace rows ran")
		return
	}
	t.Parallel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(t.Context(), "unshare", "--user", "--map-current-user", //nolint:gosec // G204: unshare and this test binary, with literal flags
		exe, "-test.run=^"+overflowTest+"$", "-test.count=1", "-test.v", overflowArg).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "the namespace rows ran") {
		t.Errorf("this test in unshare --user --map-current-user: %v:\n%s", err, out)
	}
}

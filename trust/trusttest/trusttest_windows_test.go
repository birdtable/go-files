package trusttest_test

import (
	"testing"

	"golang.org/x/sys/windows"

	"github.com/birdtable/go-files/trust/trusttest"
)

// sddl is the owner, group and access list of p, the list's control flags among them (whether it
// takes nothing from the folder above, and whether it was marked auto-inherited), as SDDL: before
// and after a change it is read by the same flags, so a put back that changed any of them shows.
func sddl(t *testing.T, p string) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return sd.String()
}

// What it costs when this fails: on Windows a test leaves a name's access list other than it
// found it, an entry added or one it inherited made its own, for the tests after it.
func TestEachChangeOnWindowsPutsBackTheAccessListItFound(t *testing.T) {
	t.Parallel()
	for _, folder := range []bool{false, true} {
		p, _ := private(t, folder)
		before := sddl(t, p)
		changes := map[string]func(r *recorder){
			"Deny": func(r *recorder) { trusttest.Deny(r, p) },
		}
		for _, who := range []trusttest.Who{trusttest.GroupReads, trusttest.GroupWrites, trusttest.OthersRead, trusttest.OthersWrite} {
			changes["Loosen "+string(rune('0'+who))] = func(r *recorder) { trusttest.Loosen(r, p, who) }
		}
		for name, change := range changes {
			r := call(t, change)
			if r.failed || r.skipped {
				t.Fatalf("%s, a folder %v: %q", name, folder, r.said)
			}
			if during := sddl(t, p); during == before {
				t.Errorf("%s, a folder %v: the list is %s, unchanged", name, folder, during)
			}
			r.end()
			if after := sddl(t, p); after != before || r.failed {
				t.Errorf("%s, a folder %v: the list is %s after, and was %s: %q", name, folder, after, before, r.said)
			}
		}
	}
}

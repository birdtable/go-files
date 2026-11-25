package trust

import "testing"

// AppContainer SIDs on Windows, judged on every system: a list that gives one write is
// trusted, and every other account in the same list is still judged.

// capability is the internetClient capability's SID, as Windows writes it in AppData's lists, and
// allPackages is ALL APPLICATION PACKAGES.
const (
	capability  = "S-1-15-3-1024-1065365936-1281604716-3511738428-1654721687-432734479-3232135806-4053264122-3456934681"
	allPackages = "S-1-15-2-1"
	aPackage    = "S-1-15-2-1430448594-2639229838-973813799-439329657-1197984847-4069167804-1277922394"
)

// What it costs when this fails: a SID that only looks like an AppContainer's is left out of the
// judging, and the account behind it changes what the program reads; or an AppContainer's is
// judged, and every program's config in AppData is refused.
func TestAnAppContainerSIDIsKnownByItsAuthorityAndFirstSubAuthority(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		sid  string
		want bool
	}{
		{capability, true},
		{allPackages, true},
		{aPackage, true},
		{"S-1-15-3-1", true},
		{"S-1-15-3", false},
		{"S-1-15-2", false},
		{"S-1-15-1-5", false},
		{"S-1-15-4-3", false},
		{"S-1-15", false},
		{"S-1-5-15-3", false},
		{"S-1-5-2-1", false},
		{"S-1-5-32-545", false},
		{"S-1-1-0", false},
		{"S-1-271-3-1", false},
		{"S-1-3840-3-1", false},
		{evilSID, false},
	} {
		if got := appContainer(sidParts(c.sid)); got != c.want {
			t.Errorf("%s: an AppContainer's is %v, and want %v", c.sid, got, c.want)
		}
	}
}

// What it costs when this fails: a program's config in %APPDATA% is refused on every ordinary
// account, since Windows gives a capability or package write to the folders it lives in.
func TestAFolderAnAppContainerMayWriteIsTrusted(t *testing.T) {
	t.Parallel()
	for _, sid := range []string{capability, allPackages, aPackage} {
		_, h := winDisk(map[string]node{
			at("a"):      winDir(userSID, allow(userSID, full, 0x3), allow(sid, full, 0x3)),
			at("a", "f"): winFile(userSID, allow(userSID, full, 0x10), allow(sid, full, 0x10)),
			at("r"):      winDir("S-1-5-18", allow("S-1-5-18", full, 0x3), allow(sid, full, 0x3)),
		})
		for _, c := range []struct {
			path string
			k    Kind
			who  Who
		}{
			{at("a"), Folder, Who{}},
			{at("a", "f"), File, Who{}},
			{at("a", "f"), Key, Who{}},
			{at("r"), Folder, Who{RootOnly: true}},
		} {
			if got, err := walk(c.path, c.k, c.who, h); err != nil || got != c.path {
				t.Errorf("%s as kind %d, %s may write, %+v: %q, %v, and want it to pass", c.path, c.k, sid, c.who, got, err)
			}
		}
	}
}

// What it costs when this fails: a folder another account may write passes because an AppContainer
// may write it too, and that account changes what the program reads.
func TestAFolderAnotherAccountMayWriteIsRefusedBesideAnAppContainer(t *testing.T) {
	t.Parallel()
	p := at("a")
	for _, other := range []string{evilSID, "S-1-5-32-545", "S-1-1-0", authed, "S-1-15-1-5", "S-1-15-4-3", "S-1-5-15-3"} {
		_, h := winDisk(map[string]node{
			p: winDir(userSID, allow(userSID, full, 0x3), allow(capability, full, 0x3), allow(allPackages, full, 0x3), allow(other, full, 0x3)),
		})
		_, err := walk(p, Folder, Who{}, h)
		wantRefused(t, other+" may write beside an AppContainer", err, refusal{ErrOthersWrite, removeGrant(p, other), "lets " + other + " change it"})
	}
}

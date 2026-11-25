package trust

import (
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"testing"
)

// The Windows rules, judged on access lists a fake host hands the walk, on every system. The
// native tests in trust_windows_test.go read the same facts from real names.

// winDir and winFile are a Windows node of owner with the access entries aces.
func winDir(owner string, aces ...ace) node {
	return node{typ: fs.ModeDir, owner: owner, dacl: &dacl{aces: aces}}
}

func winFile(owner string, aces ...ace) node { return node{owner: owner, dacl: &dacl{aces: aces}} }

// allow and deny are an entry of the type for sid with mask and flags, marked an AppContainer's
// from the SID's parts as entries marks one read from a real list.
func allow(sid string, mask uint32, flags uint8) ace {
	return ace{typ: aceAllow, flags: flags, mask: mask, sid: sid, sandbox: appContainer(sidParts(sid))}
}

func deny(sid string, mask uint32) ace {
	return ace{typ: aceDeny, mask: mask, sid: sid, sandbox: appContainer(sidParts(sid))}
}

// sidParts is the identifier authority and the sub-authorities of the SID written s, S-1-<authority>
// then each sub-authority, all decimal. It panics on any other form: only tests write one.
func sidParts(s string) ([6]byte, []uint32) {
	f := strings.Split(s, "-")
	if len(f) < 3 || f[0] != "S" || f[1] != "1" {
		panic("not a SID: " + s)
	}
	n, err := strconv.ParseUint(f[2], 10, 48)
	if err != nil {
		panic(err)
	}
	var auth [6]byte
	for i := range auth {
		auth[5-i] = byte(n >> (8 * i))
	}
	subs := make([]uint32, 0, len(f)-3)
	for _, x := range f[3:] {
		v, err := strconv.ParseUint(x, 10, 32)
		if err != nil {
			panic(err)
		}
		subs = append(subs, uint32(v))
	}
	return auth, subs
}

// full is the mask of full control, 0x1f01ff, as the probed folders hold it.
const full = 0x1f01ff

// winTop is C:\ as a probe of a real Windows install read it: TrustedInstaller's, Administrators and
// SYSTEM in full, Authenticated Users modify on what it holds (inherit-only) and add-subdirectory
// on itself.
func winTop() node {
	return winDir(installer, allow("S-1-5-32-544", full, 0x3), allow("S-1-5-18", full, 0x3),
		allow("S-1-5-32-545", 0x1200a9, 0x3), allow(authed, 0xe0010000, 0xb), allow(authed, addSubdirectory, 0))
}

// installer is TrustedInstaller's SID.
const installer = "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"

// winDisk is a fake whose top is winTop, holding nodes besides, with TrustedInstaller trusted.
func winDisk(nodes map[string]node) (*fake, host) {
	d := disk(nodes)
	d.nodes[top()] = winTop()
	h := d.host(userSID)
	h.root = append(h.root, installer)
	return d, h
}

// What it costs when this fails: on Windows a name another account owns, or one whose list lets
// another account change it, passes, and a program runs what that account wrote.
func TestEachWindowsRuleRefusesItsNameWithItsFix(t *testing.T) {
	t.Parallel()
	p := at("p")
	remove := func(sid string) Fix {
		return Fix{Path: p, Revoke: sid}
	}
	cases := []struct {
		name string
		n    node
		k    Kind
		who  Who
		r    refusal
	}{
		{"rule 1: a file of this account's", winFile(userSID, allow(userSID, full, 0)), File, Who{}, refusal{}},
		{"rule 1: a file of SYSTEM's", winFile("S-1-5-18", allow("S-1-5-18", full, 0)), File, Who{}, refusal{}},
		{"rule 1: a file of TrustedInstaller's", winFile(installer), File, Who{}, refusal{}},
		{"rule 1: a file of another's", winFile(evilSID), File, Who{}, refusal{ErrNotOwners, Fix{Path: p, Owner: administrators}, "is owned by " + evilSID}},
		{"rule 1: root alone, a file of this account's", winFile(userSID), File, Who{RootOnly: true}, refusal{ErrNotOwners, Fix{Path: p, Owner: administrators}, "not trusted"}},
		{"rule 2: another may write", winFile(userSID, allow(evilSID, 0x2, 0)), File, Who{}, refusal{ErrOthersWrite, remove(evilSID), "lets " + evilSID + " change it, with the rights 0x2"}},
		{"rule 2: everyone in full", winFile(userSID, allow("S-1-1-0", full, 0x3)), File, Who{}, refusal{ErrOthersWrite, remove("S-1-1-0"), "lets S-1-1-0 change it"}},
		{"rule 2: root alone, this account may write", winFile("S-1-5-18", allow(userSID, full, 0)), File, Who{RootOnly: true}, refusal{ErrOthersWrite, remove(userSID), "lets " + userSID}},
		{"rule 2: another may read and run", winFile(userSID, allow("S-1-5-32-545", 0x1200a9, 0)), Program, Who{}, refusal{}},
		{"rule 2: a key another may read", winFile(userSID, allow("S-1-5-32-545", 0x1200a9, 0)), Key, Who{}, refusal{ErrOthersRead, remove("S-1-5-32-545"), "lets S-1-5-32-545 read it"}},
		{"rule 2: a key another may only synchronize on and traverse", winFile(userSID, allow("S-1-15-2-1", 0x100020, 0)), Key, Who{}, refusal{}},
		{"rule 2: a private folder another may read", winDir(userSID, allow("S-1-5-32-545", 0x1200a9, 0)), PrivateFolder, Who{}, refusal{ErrOthersRead, remove("S-1-5-32-545"), "lets S-1-5-32-545 read it"}},
		{"rule 2: a private folder SYSTEM may read", winDir(userSID, allow("S-1-5-18", full, 0)), PrivateFolder, Who{}, refusal{}},
		{"rule 2: a key another may take", winFile(userSID, allow(evilSID, 0x80000, 0)), Key, Who{}, refusal{ErrOthersWrite, remove(evilSID), "change it"}},
		{"rule 2: a group's entry is not asked of Group", winFile(userSID, allow("S-1-5-32-545", 0x2, 0)), File, Who{Group: func(uint32) bool { return true }}, refusal{ErrOthersWrite, remove("S-1-5-32-545"), "change it"}},
		{"rule 3: another may write what the folder holds, not the folder", winDir(userSID, allow(evilSID, full, 0xb)), Folder, Who{}, refusal{}},
		{"rule 3: a key another may read only what it holds", winFile(userSID, allow(evilSID, full, inheritOnly)), Key, Who{}, refusal{}},
		{"a private folder whose files Users may read, (OI)(IO)", winDir(userSID, allow("S-1-5-32-545", 0x1200a9, 0x9)), PrivateFolder, Who{}, refusal{ErrOthersRead, remove("S-1-5-32-545"), "lets S-1-5-32-545 read it"}},
		{"a private folder whose every name Everyone has in full, (OI)(CI)(IO)", winDir(userSID, allow("S-1-1-0", full, 0xb)), PrivateFolder, Who{}, refusal{ErrOthersWrite, remove("S-1-1-0"), "lets S-1-1-0 change it"}},
		{"a private folder whose files' makers have them in full, CREATOR OWNER (OI)(CI)(IO)", winDir(userSID, allow(creatorOwner, full, 0xb)), PrivateFolder, Who{}, refusal{}},
		{"a private folder whose files CREATOR GROUP may read", winDir(userSID, allow("S-1-3-1", 0x1200a9, 0x9)), PrivateFolder, Who{}, refusal{ErrOthersRead, remove("S-1-3-1"), "lets S-1-3-1 read it"}},
		{"rule 4: a null list", node{owner: userSID, dacl: &dacl{null: true}}, File, Who{}, refusal{ErrOthersWrite, Fix{Path: p, Reset: true}, "has a null access list"}},
		{"an empty list, which lets none but the owner in", winFile(userSID), Key, Who{}, refusal{}},
		{"a deny entry", winFile(userSID, deny(evilSID, full)), File, Who{}, refusal{}},
		{"an entry neither allow nor deny", winFile(userSID, ace{typ: 5, mask: 0x1}), File, Who{}, refusal{ErrOthersWrite, Fix{}, "type 5"}},
		{"an audit entry in the list", winFile(userSID, ace{typ: 2, mask: full, sid: evilSID}), File, Who{}, refusal{ErrOthersWrite, Fix{}, "type 2"}},
		{"a folder where a file is asked", winDir(userSID), File, Who{}, refusal{ErrNotPlain, Fix{}, "is a folder"}},
		{"a device where a file is asked", node{typ: fs.ModeDevice, owner: userSID, dacl: &dacl{}}, File, Who{}, refusal{ErrNotPlain, Fix{}, "of type"}},
	}
	for _, c := range cases {
		_, h := winDisk(map[string]node{p: c.n})
		_, err := walk(p, c.k, c.who, h)
		wantRefused(t, c.name, err, c.r)
	}
}

// What it costs when this fails: a right that lets another account change a name, alone in its
// entry, passes, and that account changes the name.
func TestEachWriteRightAloneIsRefused(t *testing.T) {
	t.Parallel()
	p := at("p")
	refused := 0
	for bit := range 32 {
		m := uint32(1) << bit
		_, h := winDisk(map[string]node{p: winFile(userSID, allow(evilSID, m, 0))})
		_, err := walk(p, File, Who{}, h)
		if (m&writeClass != 0) != (err != nil) {
			t.Errorf("an entry giving another %s: %v", hex(m), err)
		}
		if err != nil {
			refused++
		}
	}
	if refused != 11 {
		t.Errorf("refused %d rights alone, and want the 11 of writeClass", refused)
	}
}

// What it costs when this fails: C:\, which every signed-in account may add a folder to, refuses
// every path on the system drive; or a right besides adding a folder passes there, or C:\ itself
// passes as the last name, and another account changes what the path names.
func TestAddingAFolderAlonePassesOnAFolderAboveTheLastName(t *testing.T) {
	t.Parallel()
	d, h := winDisk(map[string]node{at("Program Files"): winDir(installer, allow("S-1-5-32-545", 0x1200a9, 0))})
	if _, err := walk(at("Program Files"), Folder, Who{}, h); err != nil {
		t.Errorf("a folder of TrustedInstaller's in C:\\: %v", err)
	}
	_, err := walk(top(), Folder, Who{}, h)
	wantRefused(t, "C:\\ as the last name", err, refusal{ErrOthersWrite, removeGrant(top(), authed), "lets " + authed + " change it, with the rights 0x4"})
	for _, m := range []uint32{addSubdirectory | 0x2, addSubdirectory | 0x40, addSubdirectory | 0x10000, addSubdirectory | 0x10000000} {
		d.nodes[top()] = winDir(installer, allow(authed, m, 0))
		_, err := walk(at("Program Files"), Folder, Who{}, h)
		wantRefused(t, "C:\\ adding a folder and "+hex(m&^addSubdirectory), err, refusal{ErrOthersWrite, removeGrant(top(), authed), "with the rights " + hex(m)})
	}
	// Adding a folder with the rights to read and traverse besides is still adding alone.
	d.nodes[top()] = winDir(installer, allow(authed, addSubdirectory|0x1200a9, 0))
	if _, err := walk(at("Program Files"), Folder, Who{}, h); err != nil {
		t.Errorf("C:\\ adding a folder, read and traverse besides: %v", err)
	}
}

// What it costs when this fails: a folder another account added under C:\ on the way, which it
// owns and so may fill as it likes, passes, and the path below it names what it wrote. (The owner rule
// refuses it.)
func TestAFolderAddedOnTheWayByTheAccountThatMayAddOneIsRefusedByTheOwnerRule(t *testing.T) {
	t.Parallel()
	added, f := at("tools"), at("tools", "run.exe")
	_, h := winDisk(map[string]node{
		added: winDir(evilSID, allow(evilSID, full, 0x3), allow("S-1-5-18", full, 0x3)),
		f:     winFile(evilSID, allow(evilSID, full, 0x10)),
	})
	_, err := walk(f, Program, Who{}, h)
	wantRefused(t, "a folder another added to C:\\", err, refusal{ErrNotOwners, Fix{Path: added, Owner: administrators}, "is owned by " + evilSID})
	if u, ok := errors.AsType[*Unsafe](err); ok && !strings.HasPrefix(u.Observed, added+" ") {
		t.Errorf("the refusal names %q, and want the added folder first", u.Observed)
	}
}

// What it costs when this fails: on Windows the probed folders give another verdict here than
// they do on a real install, so a rule the fake passes is not the rule a real Windows run holds.
func TestThePartCFoldersGiveTheirRecordedVerdictsOnTheirFacts(t *testing.T) {
	t.Parallel()
	profile, git := at("Users", "u"), at("Program Files", "Git", "mingw64", "bin")
	ti := installer
	inherited := func(owner string) node {
		return winDir(owner, allow(ti, full, 0x10), allow(ti, 0x10000000, 0x1a), allow("S-1-5-18", full, 0x10), allow("S-1-5-18", 0x10000000, 0x1b),
			allow("S-1-5-32-544", full, 0x10), allow("S-1-5-32-544", 0x10000000, 0x1b), allow("S-1-3-0", 0x10000000, 0x1b),
			allow("S-1-5-32-545", 0x1200a9, 0x10), allow("S-1-15-2-1", 0x1200a9, 0x10))
	}
	_, h := winDisk(map[string]node{
		at("Users"):                           winDir("S-1-5-18", allow("S-1-5-18", full, 0x3), allow("S-1-5-32-544", full, 0x3), allow("S-1-1-0", 0x1200a9, 0x3)),
		profile:                               winDir("S-1-5-18", allow("S-1-5-18", full, 0x3), allow("S-1-5-32-544", full, 0x3), allow(userSID, full, 0x3), allow("S-1-15-2-1", 0x100020, 0)),
		at("Program Files"):                   inherited(ti),
		at("Program Files", "Git"):            inherited("S-1-5-32-544"),
		at("Program Files", "Git", "mingw64"): inherited("S-1-5-32-544"),
		git:                                   inherited("S-1-5-32-544"),
	})
	for _, c := range []struct {
		path string
		who  Who
		r    refusal
	}{
		{profile, Who{}, refusal{}},
		{git, Who{}, refusal{}},
		{git, Who{RootOnly: true}, refusal{}},
		{profile, Who{RootOnly: true}, refusal{ErrOthersWrite, removeGrant(profile, userSID), "lets " + userSID}},
		{top(), Who{}, refusal{ErrOthersWrite, removeGrant(top(), authed), "lets " + authed}},
	} {
		_, err := walk(c.path, Folder, c.who, h)
		wantRefused(t, c.path, err, c.r)
	}
}

// What it costs when this fails: ReadOwned on Windows reads a file another account can change, or
// a private file another can read, and hands its caller what that account wrote.
func TestReadOwnedJudgesAWindowsListOnItsDescriptor(t *testing.T) {
	t.Parallel()
	_, h := winDisk(nil)
	p := at("f")
	cases := []struct {
		name, owner string
		n           node
		private     bool
		r           refusal
	}{
		{"the owner's own entry", userSID, winFile(userSID, allow(userSID, full, 0)), true, refusal{}},
		{"SYSTEM and Administrators in full", userSID, winFile(userSID, allow("S-1-5-18", full, 0), allow("S-1-5-32-544", full, 0)), true, refusal{}},
		{"this account, which the walk trusts and the owner is not", evilSID, winFile(evilSID, allow(userSID, 0x2, 0)), false, refusal{ErrOthersWrite, removeGrant(p, userSID), "change it"}},
		{"another may write", userSID, winFile(userSID, allow(evilSID, 0x2, 0)), false, refusal{ErrOthersWrite, removeGrant(p, evilSID), "change it"}},
		{"another may read, private", userSID, winFile(userSID, allow("S-1-5-32-545", 0x1200a9, 0)), true, refusal{ErrOthersRead, removeGrant(p, "S-1-5-32-545"), "read it"}},
		{"another may read, not private", userSID, winFile(userSID, allow("S-1-5-32-545", 0x1200a9, 0)), false, refusal{}},
		{"a null list", userSID, node{owner: userSID, dacl: &dacl{null: true}}, false, refusal{ErrOthersWrite, Fix{Path: p, Reset: true}, "null"}},
		{"another owner", userSID, winFile("S-1-5-18"), false, refusal{ErrNotOwners, Fix{}, "not by " + userSID}},
		{"an AppContainer may write and read, private", userSID, winFile(userSID, allow(userSID, full, 0), allow(capability, full, 0), allow(allPackages, full, 0)), true, refusal{}},
		{"another may write beside an AppContainer", userSID, winFile(userSID, allow(capability, full, 0), allow("S-1-5-32-545", 0x2, 0)), false, refusal{ErrOthersWrite, removeGrant(p, "S-1-5-32-545"), "change it"}},
		{"a SID only like an AppContainer's may write", userSID, winFile(userSID, allow("S-1-5-15-3", 0x2, 0)), false, refusal{ErrOthersWrite, removeGrant(p, "S-1-5-15-3"), "change it"}},
	}
	for _, c := range cases {
		wantRefused(t, c.name, judgeRead(c.n, p, c.owner, c.private, 10, h), c.r)
	}
}

// What it costs when this fails: a junction of Administrators' whose own list lets another account
// change it passes, and that account points it elsewhere and chooses what the path names. A folder
// at the same name with the same list is refused; the link is held as a folder is, and the right to
// add a folder alone passes on no link.
func TestAWindowsLinkAnotherAccountMayChangeIsRefusedBeforeItIsFollowed(t *testing.T) {
	t.Parallel()
	j, f := at("j"), at("j", "f")
	winLink := func(d *dacl) node { return node{typ: fs.ModeSymlink, owner: "S-1-5-32-544", dacl: d} }
	cases := []struct {
		name string
		d    *dacl
		r    refusal
	}{
		{"a null list", &dacl{null: true}, refusal{ErrOthersWrite, Fix{Path: j, Reset: true}, "has a null access list"}},
		{"Everyone in full", &dacl{aces: []ace{allow("S-1-1-0", full, 0)}}, refusal{ErrOthersWrite, removeGrant(j, "S-1-1-0"), "lets S-1-1-0 change it"}},
		{"Authenticated Users may modify", &dacl{aces: []ace{allow(authed, 0x1301bf, 0)}}, refusal{ErrOthersWrite, removeGrant(j, authed), "lets " + authed + " change it, with the rights 0x1301bf"}},
		{"another may only add a folder", &dacl{aces: []ace{allow(authed, addSubdirectory, 0)}}, refusal{ErrOthersWrite, removeGrant(j, authed), "with the rights 0x4"}},
		{"an entry neither allow nor deny", &dacl{aces: []ace{{typ: 5, mask: 0x1}}}, refusal{ErrOthersWrite, Fix{}, "type 5"}},
		{"root's, this account's and an AppContainer's", &dacl{aces: []ace{allow("S-1-5-32-544", full, 0), allow(userSID, full, 0), allow(capability, full, 0)}}, refusal{}},
		{"Everyone in full on what it holds only", &dacl{aces: []ace{allow("S-1-1-0", full, inheritOnly)}}, refusal{}},
	}
	for _, c := range cases {
		d, h := winDisk(map[string]node{j: winLink(c.d), at("t"): winDir(userSID), at("t", "f"): winFile(userSID)})
		d.links[j] = at("t")
		_, err := walk(f, File, Who{}, h)
		wantRefused(t, c.name, err, c.r)
		if c.r != (refusal{}) && len(d.read) != 0 {
			t.Errorf("%s: read the link %v before it passed", c.name, d.read)
		}
	}
}

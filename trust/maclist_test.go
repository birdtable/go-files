package trust

import (
	"errors"
	"maps"
	"testing"
)

// listedAs is n holding the macOS access list aces.
func listedAs(n node, aces ...macACE) node { n.mac = aces; return n }

// allowMac and denyMac are a macOS entry that allows, or denies, who the rights.
func allowMac(who string, rights uint32) macACE {
	return macACE{who: who, flags: macPermit, rights: rights}
}
func denyMac(who string, rights uint32) macACE {
	return macACE{who: who, flags: macDeny, rights: rights}
}

const (
	macWriteData = 1 << 2   // KAUTH_VNODE_WRITE_DATA
	macReadData  = 1 << 1   // KAUTH_VNODE_READ_DATA
	macExecute   = 1 << 3   // KAUTH_VNODE_EXECUTE
	macDelete    = 1 << 4   // KAUTH_VNODE_DELETE
	staffID      = "gid:20" // the group of every account a Mac makes
	everyoneID   = "gid:12" // the group every account is in
	someGUID     = "guid:0A1B2C3D-4E5F-6071-8293-A4B5C6D7E8F9"
)

// What it costs when this fails: on macOS a file, key, program or folder whose access list lets
// another account write it passes, and that account changes what the program runs or reads;
// or a home folder, whose list denies everyone its deletion, is refused.
func TestEachRuleOfAMacListRefusesItsNameWithItsFix(t *testing.T) {
	t.Parallel()
	p := at("p")
	fix := func(who string) Fix { return Fix{Path: p, Revoke: who} }
	ours := func(gid uint32) bool { return gid == 20 }
	cases := []struct {
		name string
		n    node
		k    Kind
		who  Who
		r    refusal
	}{
		{"a file with no list", listedAs(file(meID, 0o644)), File, Who{}, refusal{}},
		{"its group may write it", listedAs(file(meID, 0o644), allowMac(staffID, macWriteData)), File, Who{}, refusal{ErrOthersWrite, fix(staffID), "lets " + staffID + " change it, with the rights 0x4"}},
		{"its group may write it, of a group asked", listedAs(file(meID, 0o644), allowMac(staffID, macWriteData)), File, Who{Group: ours}, refusal{ErrOthersWrite, fix(staffID), "lets " + staffID + " change it"}},
		{"another account may write it", listedAs(file(meID, 0o644), allowMac(otherID, macWriteData)), File, Who{}, refusal{ErrOthersWrite, fix(otherID), "lets " + otherID}},
		{"an account by its own GUID may write it", listedAs(file(meID, 0o644), allowMac(someGUID, macWriteData)), File, Who{}, refusal{ErrOthersWrite, fix(someGUID), "lets " + someGUID}},
		{"this account may write it", listedAs(file(meID, 0o644), allowMac(meID, macWriteData)), File, Who{}, refusal{}},
		{"root may write it", listedAs(file(meID, 0o644), allowMac(rootID, macWriteData)), File, Who{}, refusal{}},
		{"root alone: this account may write it", listedAs(file(rootID, 0o644), allowMac(meID, macWriteData)), File, Who{RootOnly: true}, refusal{ErrOthersWrite, fix(meID), "lets " + meID}},
		{"the second entry lets another write it", listedAs(file(meID, 0o644), allowMac(meID, macWriteData), allowMac(otherID, macDelete)), File, Who{}, refusal{ErrOthersWrite, fix(otherID), "lets " + otherID}},
		{"its group is denied writing it", listedAs(file(meID, 0o644), denyMac(staffID, macWriteData)), File, Who{}, refusal{}},
		{"an entry inherited by what is made in it alone", listedAs(dir(meID, 0o755), macACE{who: staffID, flags: macPermit | macOnlyInherit, rights: macWriteData}), Folder, Who{}, refusal{}},
		{"an entry of a kind not read", listedAs(file(meID, 0o644), macACE{who: staffID, flags: 3, rights: macReadData}), File, Who{}, refusal{ErrOthersWrite, fix(staffID), "of kind 3"}},
		{"an entry of a kind not read, for this account", listedAs(file(meID, 0o644), macACE{who: meID, flags: 0, rights: 0}), File, Who{}, refusal{ErrOthersWrite, fix(meID), "of kind 0"}},
		{"its group may read and run a file", listedAs(file(meID, 0o644), allowMac(staffID, macReadData|macExecute)), File, Who{}, refusal{}},
		{"its group may read a key", listedAs(file(meID, 0o600), allowMac(staffID, macReadData)), Key, Who{}, refusal{ErrOthersRead, fix(staffID), "lets " + staffID + " read it"}},
		{"its group may run a key", listedAs(file(meID, 0o600), allowMac(staffID, macExecute)), Key, Who{}, refusal{}},
		{"its group may read a private folder", listedAs(dir(meID, 0o700), allowMac(staffID, macReadData)), PrivateFolder, Who{}, refusal{ErrOthersRead, fix(staffID), "lets " + staffID + " read it"}},
		{"its group may search a private folder", listedAs(dir(meID, 0o700), allowMac(staffID, macExecute)), PrivateFolder, Who{}, refusal{ErrOthersRead, fix(staffID), "lets " + staffID + " search it"}},
		{"another account may search a private folder by GENERIC_EXECUTE", listedAs(dir(meID, 0o700), allowMac(otherID, 1<<22)), PrivateFolder, Who{}, refusal{ErrOthersRead, fix(otherID), "lets " + otherID + " search it"}},
		{"its group is denied search on a private folder", listedAs(dir(meID, 0o700), denyMac(staffID, macExecute)), PrivateFolder, Who{}, refusal{}},
		{"its group may search a folder", listedAs(dir(meID, 0o755), allowMac(staffID, macExecute)), Folder, Who{}, refusal{}},
		{"a private folder whose files its group may read", listedAs(dir(meID, 0o700), macACE{who: staffID, flags: macPermit | 1<<5 | macOnlyInherit, rights: macReadData}), PrivateFolder, Who{}, refusal{ErrOthersRead, fix(staffID), "lets " + staffID + " read it"}},
		{"a private folder whose folders its group may search", listedAs(dir(meID, 0o700), macACE{who: staffID, flags: macPermit | 1<<6 | macOnlyInherit, rights: macExecute}), PrivateFolder, Who{}, refusal{ErrOthersRead, fix(staffID), "lets " + staffID + " search it"}},
		{"a key: an entry for what is made in it alone", listedAs(file(meID, 0o600), macACE{who: staffID, flags: macPermit | macOnlyInherit, rights: macReadData}), Key, Who{}, refusal{}},
		{"a home folder: everyone is denied its deletion", listedAs(dir(meID, 0o755), denyMac(everyoneID, macDelete)), Folder, Who{}, refusal{}},
		{"its mode is judged first", listedAs(file(meID, 0o666), allowMac(staffID, macWriteData)), File, Who{}, refusal{ErrOthersWrite, Fix{Path: p, Clear: 0o022}, "its group and others"}},
		{"a list not read", node{owner: meID, perm: 0o644, macUnread: "why"}, File, Who{}, refusal{ErrOthersWrite, Fix{}, "not read here: why"}},
		{"a list not read, its mode judged first", node{owner: meID, perm: 0o664, macUnread: "why"}, File, Who{}, refusal{ErrOthersWrite, Fix{Path: p, Clear: 0o022}, "its group"}},
		{"a list not read, its owner judged first", node{owner: otherID, perm: 0o644, macUnread: "why"}, File, Who{}, refusal{ErrNotOwners, Fix{Path: p, Owner: "uid:0"}, "is owned by"}},
	}
	for _, c := range cases {
		d := disk(map[string]node{p: c.n})
		got, err := walk(p, c.k, c.who, d.host(meID))
		wantRefused(t, c.name, err, c.r)
		if err == nil && got != p {
			t.Errorf("%s: answered %q, and want %q", c.name, got, p)
		}
	}
}

// What it costs when this fails: a right that changes a name, granted to another account by a
// macOS list, passes, and that account renames, rewrites, deletes or takes the name.
func TestEachRightThatChangesANameIsRefusedInAMacList(t *testing.T) {
	t.Parallel()
	p := at("p")
	for bit := range 32 {
		r := uint32(1) << bit
		d := disk(map[string]node{p: listedAs(file(meID, 0o644), allowMac(otherID, r))})
		_, err := walk(p, File, Who{}, d.host(meID))
		want := refusal{}
		if r&macWrite != 0 {
			want = refusal{ErrOthersWrite, Fix{Path: p, Revoke: otherID}, "change it"}
		}
		wantRefused(t, "the right "+hex(r), err, want)
	}
	for _, r := range []uint32{1 << 2, 1 << 4, 1 << 5, 1 << 6, 1 << 8, 1 << 10, 1 << 12, 1 << 13, 1 << 21, 1 << 23} {
		if r&macWrite == 0 {
			t.Errorf("the right %s, which changes a name, is not refused", hex(r))
		}
	}
}

// What it costs when this fails: a link whose own macOS list lets another account change it
// passes, and that account repoints the link once the walk has read it; or its fix leaves the
// link's list as it was, changing its target's.
func TestALinkWhoseMacListLetsAnotherChangeItIsRefusedBeforeItIsRead(t *testing.T) {
	t.Parallel()
	l, f := at("l"), at("f")
	ln := link(meID)
	ln.mac = []macACE{allowMac(otherID, macWriteData)}
	d := disk(map[string]node{l: ln, f: file(meID, 0o644)})
	d.links[l] = "f"
	_, err := walk(l, File, Who{}, d.host(meID))
	wantRefused(t, "a link another may change", err, refusal{ErrOthersWrite, Fix{Path: l, Revoke: otherID}, "lets " + otherID})
	if len(d.read) != 0 {
		t.Errorf("read the link %v before it passed", d.read)
	}
	nodes := maps.Clone(d.nodes)
	nodes[l] = listedAs(link(meID), denyMac(everyoneID, macDelete))
	d = disk(nodes)
	d.links[l] = "f"
	_, err = walk(l, File, Who{}, d.host(meID))
	wantRefused(t, "a link everyone is denied deleting", err, refusal{})
}

// What it costs when this fails: ReadOwned on macOS reads a file whose access list lets another
// account write it, or read it when private.
func TestReadOwnedJudgesAMacListOnItsDescriptor(t *testing.T) {
	t.Parallel()
	h := disk(nil).host(meID)
	p := at("f")
	fix := func(who string) Fix { return Fix{Path: p, Revoke: who} }
	cases := []struct {
		name, owner string
		n           node
		private     bool
		r           refusal
	}{
		{"no list", meID, listedAs(file(meID, 0o600)), true, refusal{}},
		{"the owner's own entry", meID, listedAs(file(meID, 0o600), allowMac(meID, macWriteData|macReadData)), true, refusal{}},
		{"root's entry", meID, listedAs(file(meID, 0o600), allowMac(rootID, macWriteData|macReadData)), true, refusal{}},
		{"this account, which the walk trusts and the owner is not", otherID, listedAs(file(otherID, 0o600), allowMac(meID, macWriteData)), false, refusal{ErrOthersWrite, fix(meID), "change it"}},
		{"its group may write", meID, listedAs(file(meID, 0o600), allowMac(staffID, macWriteData)), false, refusal{ErrOthersWrite, fix(staffID), "lets " + staffID + " change it"}},
		{"its group may read, private", meID, listedAs(file(meID, 0o600), allowMac(staffID, macReadData)), true, refusal{ErrOthersRead, fix(staffID), "read it"}},
		{"its group may read, not private", meID, listedAs(file(meID, 0o600), allowMac(staffID, macReadData)), false, refusal{}},
		{"an entry of a kind not read", meID, listedAs(file(meID, 0o600), macACE{who: staffID, flags: 4}), false, refusal{ErrOthersWrite, fix(staffID), "of kind 4"}},
		{"a list not read", meID, node{owner: meID, perm: 0o600, macUnread: "why"}, false, refusal{ErrOthersWrite, Fix{}, "not read here: why"}},
		{"a list not read, its mode judged first", meID, node{owner: meID, perm: 0o666, macUnread: "why"}, false, refusal{ErrOthersWrite, Fix{Path: p, Clear: 0o022}, "written by"}},
	}
	for _, c := range cases {
		err := judgeRead(c.n, p, c.owner, c.private, 10, h)
		wantRefused(t, c.name, err, c.r)
		if c.n.macUnread != "" && c.r.fix == (Fix{}) && !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("%s: %v does not match errors.ErrUnsupported", c.name, err)
		}
	}
}

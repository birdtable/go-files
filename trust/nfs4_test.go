package trust

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
)

// nfsACE is one entry of an NFSv4 list to encode.
type nfsACE struct {
	typ, flags, mask uint32
	who              string
}

// xdr is the list es as system.nfs4_acl holds it.
func xdr(es ...nfsACE) []byte {
	b := binary.BigEndian.AppendUint32(nil, uint32(len(es)))
	for _, e := range es {
		b = binary.BigEndian.AppendUint32(b, e.typ)
		b = binary.BigEndian.AppendUint32(b, e.flags)
		b = binary.BigEndian.AppendUint32(b, e.mask)
		b = binary.BigEndian.AppendUint32(b, uint32(len(e.who)))
		b = append(b, e.who...)
		b = append(b, make([]byte, (4-len(e.who)%4)%4)...)
	}
	return b
}

const alice = "alice@example.com" // a named account an NFSv4 server keeps

// What it costs when this fails: an NFSv4 entry is read as another account's than the one it
// names, so a named account's grant passes as the owner's, or the owner's own is refused; or a
// list cut short is judged on what was read of it.
func TestAnNFSv4ListIsReadAsItsEntries(t *testing.T) {
	t.Parallel()
	n := dir(meID, 0o700)
	n.gid = 20
	got := nfs4List(n, xdr(
		nfsACE{nfsAllow, 0, 0x1f01ff, "OWNER@"},
		nfsACE{nfsAllow, nfsGroupWho, 0x1, "GROUP@"},
		nfsACE{nfsDeny, 0, 0x2, "EVERYONE@"},
		nfsACE{nfsAllow, nfsOnlyInherit | 0x1, 0x1, alice},
		nfsACE{nfsAllow, nfsGroupWho, 0x1, "staff@example.com"},
		nfsACE{2, 0, 0x1f01ff, alice},
		nfsACE{nfsAlarm, 0, 0x1f01ff, alice},
		nfsACE{nfsAllow, nfsGroupWho, 0x1, "OWNER@"},
	))
	want := []macACE{
		{meID, macPermit, 0x1f01ff},
		{"gid:20", macPermit, 0x1},
		{"nfs4:EVERYONE@", macDeny, 0x2},
		{"nfs4:" + alice, macPermit | macOnlyInherit, 0x1},
		{"nfs4group:staff@example.com", macPermit, 0x1},
		{"nfs4group:OWNER@", macPermit, 0x1},
	}
	if !got.nfs4 || got.macUnread != "" || !slices.Equal(got.mac, want) {
		t.Errorf("read %+v (unread %q), and want %+v", got.mac, got.macUnread, want)
	}
	whole := xdr(nfsACE{nfsAllow, 0, 0x1, alice})
	for _, c := range []struct {
		name string
		b    []byte
		says string
	}{
		{"an entry of a type not read", xdr(nfsACE{4, 0, 0x1, alice}), "an NFSv4 entry of type 4"},
		{"a list cut in its name", whole[:len(whole)-1], "does not parse"},
		{"a list cut in its count", whole[:3], "does not parse"},
		{"a list longer than its count", append(slices.Clone(whole), 0, 0, 0, 0), "does not parse"},
		{"a count past the list", append([]byte{0, 0, 0, 2}, whole[4:]...), "does not parse"},
	} {
		got := nfs4List(n, c.b)
		if !strings.Contains(got.macUnread, c.says) || got.mac != nil {
			t.Errorf("%s: read %+v, unread %q, and want it unread, %q", c.name, got.mac, got.macUnread, c.says)
		}
	}
}

// What it costs when this fails: on an NFSv4 mount a key or a PrivateFolder at 0700 whose
// server's list lets a named account read it passes, or a file another may change by that list,
// as the mode does not show it.
func TestEachRuleOfAnNFSv4ListRefusesItsNameWithItsFix(t *testing.T) {
	t.Parallel()
	p := at("p")
	fix := func(who string) Fix { return Fix{Path: p, Revoke: who} }
	nfs := func(n node, es ...nfsACE) node { return nfs4List(n, xdr(es...)) }
	readAttrs := uint32(0x120080) // READ_ATTRIBUTES, READ_ACL and SYNCHRONIZE, as a server gives EVERYONE@
	unshown := func(n node) node { n.nfsUnshown = true; return n }
	cases := []struct {
		name string
		n    node
		k    Kind
		r    refusal
	}{
		{"a private folder a named account may read", nfs(dir(meID, 0o700), nfsACE{nfsAllow, 0, nfsRead, alice}), PrivateFolder, refusal{ErrOthersRead, fix("nfs4:" + alice), "lets nfs4:" + alice + " read it, with the rights 0x1"}},
		{"a key a named account may read", nfs(file(meID, 0o600), nfsACE{nfsAllow, 0, nfsRead, alice}), Key, refusal{ErrOthersRead, fix("nfs4:" + alice), "read it"}},
		{"a key a named group may read", nfs(file(meID, 0o600), nfsACE{nfsAllow, nfsGroupWho, nfsRead, "staff@example.com"}), Key, refusal{ErrOthersRead, fix("nfs4group:staff@example.com"), "read it"}},
		{"a private folder everyone may search", nfs(dir(meID, 0o700), nfsACE{nfsAllow, 0, nfsSearch, "EVERYONE@"}), PrivateFolder, refusal{ErrOthersRead, fix("nfs4:EVERYONE@"), "search it, with the rights 0x20"}},
		{"a private folder whose files a named account may read", nfs(dir(meID, 0o700), nfsACE{nfsAllow, 0x1 | nfsOnlyInherit, nfsRead, alice}), PrivateFolder, refusal{ErrOthersRead, fix("nfs4:" + alice), "read it"}},
		{"a file a named account may write", nfs(file(meID, 0o644), nfsACE{nfsAllow, 0, 0x2, alice}), File, refusal{ErrOthersWrite, fix("nfs4:" + alice), "change it, with the rights 0x2"}},
		{"a file a named account may take", nfs(file(meID, 0o644), nfsACE{nfsAllow, 0, 0x80000, alice}), File, refusal{ErrOthersWrite, fix("nfs4:" + alice), "change it"}},
		{"a file its owner's name may write, as no name is trusted", nfs(file(meID, 0o644), nfsACE{nfsAllow, 0, 0x2, "me@example.com"}), File, refusal{ErrOthersWrite, fix("nfs4:me@example.com"), "change it"}},
		{"a file a named account may read", nfs(file(meID, 0o644), nfsACE{nfsAllow, 0, nfsRead, alice}), File, refusal{}},
		{"a key: the owner's entry in full, everyone's attributes", nfs(file(meID, 0o600), nfsACE{nfsAllow, 0, 0x1f01ff, "OWNER@"}, nfsACE{nfsAllow, 0, readAttrs, "EVERYONE@"}), Key, refusal{}},
		{"a key: a named account denied", nfs(file(meID, 0o600), nfsACE{nfsDeny, 0, 0x1f01ff, alice}), Key, refusal{}},
		{"a folder whose files a named account may write", nfs(dir(meID, 0o755), nfsACE{nfsAllow, 0x1 | nfsOnlyInherit, 0x2, alice}), Folder, refusal{}},
		{"a list that does not parse", nfs4List(file(meID, 0o644), []byte{0, 0, 0, 1}), File, refusal{ErrOthersWrite, Fix{}, "not read here: an NFSv4 list that does not parse"}},
		{"a key a named account may run, which reads it at the server", nfs(file(meID, 0o600), nfsACE{nfsAllow, 0, nfsSearch, alice}), Key, refusal{ErrOthersRead, fix("nfs4:" + alice), "lets nfs4:" + alice + " read it, with the rights 0x20"}},
		{"a key everyone may run", nfs(file(meID, 0o600), nfsACE{nfsAllow, 0, nfsSearch, "EVERYONE@"}), Key, refusal{ErrOthersRead, fix("nfs4:EVERYONE@"), "read it"}},
		{"a file a named account may run", nfs(file(meID, 0o644), nfsACE{nfsAllow, 0, nfsSearch, alice}), File, refusal{}},
		{"a key on a mount that cannot show its list", unshown(file(meID, 0o600)), Key, refusal{ErrOthersRead, Fix{}, "cannot show the server's access list"}},
		{"a private folder on a mount that cannot show its list", unshown(dir(meID, 0o700)), PrivateFolder, refusal{ErrOthersRead, Fix{}, "cannot show the server's access list"}},
		{"a file on a mount that cannot show its list", unshown(file(meID, 0o644)), File, refusal{}},
		{"a folder on a mount that cannot show its list", unshown(dir(meID, 0o755)), Folder, refusal{}},
	}
	// RFC 8881 names: WRITE_DATA, APPEND_DATA, WRITE_NAMED_ATTRS, DELETE_CHILD, WRITE_ATTRIBUTES,
	// WRITE_RETENTION, WRITE_RETENTION_HOLD, DELETE, WRITE_ACL and WRITE_OWNER.
	for _, bit := range []uint32{0x2, 0x4, 0x10, 0x40, 0x100, 0x200, 0x400, 0x10000, 0x40000, 0x80000} {
		cases = append(cases, struct {
			name string
			n    node
			k    Kind
			r    refusal
		}{"a file a named account may write, with the right " + hex(bit), nfs(file(meID, 0o644), nfsACE{nfsAllow, 0, bit, alice}), File, refusal{ErrOthersWrite, fix("nfs4:" + alice), "change it, with the rights " + hex(bit)}})
	}
	for _, c := range cases {
		d := disk(map[string]node{p: c.n})
		_, err := walk(p, c.k, Who{}, d.host(meID))
		wantRefused(t, c.name, err, c.r)
	}
}

// What it costs when this fails: ReadOwned reads a private file on an NFSv4 mount whose list lets
// a named account read it.
func TestReadOwnedJudgesAnNFSv4ListOnItsDescriptor(t *testing.T) {
	t.Parallel()
	h := disk(nil).host(meID)
	p := at("f")
	n := nfs4List(file(meID, 0o600), xdr(nfsACE{nfsAllow, 0, 0x1f01ff, "OWNER@"}, nfsACE{nfsAllow, 0, nfsRead, alice}))
	wantRefused(t, "private", judgeRead(n, p, meID, true, 10, h), refusal{ErrOthersRead, Fix{Path: p, Revoke: "nfs4:" + alice}, "read it"})
	wantRefused(t, "not private", judgeRead(n, p, meID, false, 10, h), refusal{})
	run := nfs4List(file(meID, 0o600), xdr(nfsACE{nfsAllow, 0, 0x1f01ff, "OWNER@"}, nfsACE{nfsAllow, 0, nfsSearch, alice}))
	wantRefused(t, "private, run by another", judgeRead(run, p, meID, true, 10, h), refusal{ErrOthersRead, Fix{Path: p, Revoke: "nfs4:" + alice}, "read it"})
	v3 := file(meID, 0o600)
	v3.nfsUnshown = true
	err := judgeRead(v3, p, meID, true, 10, h)
	wantRefused(t, "private, on a mount that cannot show its list", err, refusal{ErrOthersRead, Fix{}, "cannot show the server's access list"})
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("on a mount that cannot show its list: %v, and want errors.ErrUnsupported", err)
	}
	wantRefused(t, "not private, on a mount that cannot show its list", judgeRead(v3, p, meID, false, 10, h), refusal{})
}

// What it costs when this fails: on an NFSv4 mount knfsd derives every name's list from its mode,
// so a key below a shared sticky folder of root's, or in a folder a group Who.Group lets in may
// write, is refused where the mode passes it; or a list that gives more than the mode shows passes.
func TestAnNFSv4ListDerivedFromTheModeIsJudgedAsTheMode(t *testing.T) {
	t.Parallel()
	o, f := at("o"), at("o", "f")
	const derived = 0x1200e7 // rwaDxtcy, knfsd's GROUP@ and EVERYONE@ for rwx
	ours := Who{Group: func(g uint32) bool { return g == 20 }}
	tmp := func(everyone uint32) node {
		return nfs4List(dir(rootID, fs.ModeSticky|0o777), xdr(nfsACE{nfsAllow, 0, 0x1f01ff, "OWNER@"}, nfsACE{nfsAllow, nfsGroupWho, derived, "GROUP@"}, nfsACE{nfsAllow, 0, everyone, "EVERYONE@"}))
	}
	shared := func(mode fs.FileMode, group, everyone uint32) node {
		n := dir(meID, mode)
		n.gid = 20
		return nfs4List(n, xdr(nfsACE{nfsAllow, 0, 0x1f01ff, "OWNER@"}, nfsACE{nfsAllow, nfsGroupWho, group, "GROUP@"}, nfsACE{nfsAllow, 0, everyone, "EVERYONE@"}))
	}
	cases := []struct {
		name string
		n    node
		w    Who
		r    refusal
	}{
		{"root's sticky folder above, as knfsd lists it", tmp(derived), Who{}, refusal{}},
		{"the same, everyone given WRITE_ACL too", tmp(derived | 0x40000), Who{}, refusal{ErrOthersWrite, Fix{Path: o, Revoke: "nfs4:EVERYONE@"}, "lets nfs4:EVERYONE@ change it"}},
		{"a folder of a group asked, as knfsd lists it", shared(0o775, derived, 0x1200a1), ours, refusal{}},
		{"the same, the group given WRITE_OWNER too", shared(0o775, derived|0x80000, 0x1200a1), ours, refusal{ErrOthersWrite, Fix{Path: o, Revoke: "gid:20"}, "lets gid:20 change it"}},
		{"a list that gives everyone a write its mode does not show", shared(0o775, derived, derived), ours, refusal{ErrOthersWrite, Fix{Path: o, Revoke: "nfs4:EVERYONE@"}, "lets nfs4:EVERYONE@ change it"}},
		{"a list that gives the group a write its mode does not show", shared(0o755, derived, 0x1200a1), ours, refusal{ErrOthersWrite, Fix{Path: o, Revoke: "gid:20"}, "lets gid:20 change it"}},
	}
	for _, c := range cases {
		d := disk(map[string]node{o: c.n, f: dir(meID, 0o700)})
		_, err := walk(f, PrivateFolder, c.w, d.host(meID))
		wantRefused(t, c.name, err, c.r)
	}
}

package trust

import (
	"encoding/binary"
	"strconv"
)

// An NFSv4 list, Linux's system.nfs4_acl, is judged by judgeMac in its own rights on every system.
// The values are RFC 8881's.
const (
	nfsAllow       = 0   // ACE4_ACCESS_ALLOWED_ACE_TYPE
	nfsDeny        = 1   // ACE4_ACCESS_DENIED_ACE_TYPE
	nfsAlarm       = 3   // ACE4_SYSTEM_ALARM_ACE_TYPE: like an audit entry, it grants nothing
	nfsOnlyInherit = 0x8 // ACE4_INHERIT_ONLY_ACE
	nfsGroupWho    = 0x40

	// nfsWrite: WRITE_DATA, APPEND_DATA, WRITE_NAMED_ATTRS, DELETE_CHILD, WRITE_ATTRIBUTES,
	// WRITE_RETENTION, WRITE_RETENTION_HOLD, DELETE, WRITE_ACL and WRITE_OWNER.
	nfsWrite = 0x2 | 0x4 | 0x10 | 0x40 | 0x100 | 0x200 | 0x400 | 0x10000 | 0x40000 | 0x80000
	// nfsAddOnly is the write a mode's w bit gives: WRITE_DATA, APPEND_DATA and DELETE_CHILD.
	nfsAddOnly = 0x2 | 0x4 | 0x40
	nfsRead    = 0x1  // READ_DATA (LIST_DIRECTORY)
	nfsSearch  = 0x20 // EXECUTE
)

// nfs4List is n holding the XDR list b as macOS entries in NFSv4's rights, named as the README
// says. A list that does not parse, or holds an entry of a type past alarm, is not read.
func nfs4List(n node, b []byte) node {
	n.nfs4 = true
	count, b, ok := xdrWord(b)
	for i := uint32(0); ok && i < count; i++ {
		var typ, flags, mask uint32
		var who string
		typ, b, _ = xdrWord(b) // a word cut short leaves b empty, and the name fails
		flags, b, _ = xdrWord(b)
		mask, b, _ = xdrWord(b)
		who, b, ok = xdrString(b)
		switch {
		case !ok:
		case typ > nfsAlarm:
			n.macUnread = "an NFSv4 entry of type " + strconv.FormatUint(uint64(typ), 10)
			return n
		case typ == nfsAllow || typ == nfsDeny:
			e := macACE{who: nfsWho(n, who, flags), flags: macPermit, rights: mask}
			if typ == nfsDeny {
				e.flags = macDeny
			}
			if flags&nfsOnlyInherit != 0 {
				e.flags |= macOnlyInherit
			}
			n.mac = append(n.mac, e)
		}
	}
	if !ok || len(b) != 0 {
		n.mac, n.macUnread = nil, "an NFSv4 list that does not parse"
	}
	return n
}

func nfsWho(n node, who string, flags uint32) string {
	switch {
	case who == "OWNER@" && flags&nfsGroupWho == 0:
		return n.owner
	case who == "GROUP@":
		return "gid:" + strconv.FormatUint(uint64(n.gid), 10)
	case flags&nfsGroupWho != 0:
		return "nfs4group:" + who
	}
	return "nfs4:" + who
}

func xdrWord(b []byte) (uint32, []byte, bool) {
	if len(b) < 4 {
		return 0, nil, false
	}
	return binary.BigEndian.Uint32(b), b[4:], true
}

// xdrString is the string, its length first and padded to 4 bytes, that starts b.
func xdrString(b []byte) (string, []byte, bool) {
	l, b, ok := xdrWord(b)
	padded := (uint64(l) + 3) &^ 3
	if !ok || uint64(len(b)) < padded {
		return "", nil, false
	}
	return string(b[:l]), b[padded:], true
}

// nfsModeWrite is nfsAddOnly for an allow entry of GROUP@ or EVERYONE@ whose class's w bit n's
// mode shows, which judgeMode has judged: knfsd derives such an entry from every mode.
func nfsModeWrite(n node, e macACE) uint32 {
	group := e.who == "gid:"+strconv.FormatUint(uint64(n.gid), 10) && n.perm&0o020 != 0
	if e.flags&macKindMask == macPermit && (group || e.who == "nfs4:EVERYONE@" && n.perm&0o002 != 0) {
		return nfsAddOnly
	}
	return 0
}

// nfsReads is READ_DATA and EXECUTE for any reads: a server reads a file to EXECUTE alone, and a
// private folder is refused both.
func nfsReads(reads uint32) uint32 {
	if reads != 0 {
		return nfsRead | nfsSearch
	}
	return 0
}

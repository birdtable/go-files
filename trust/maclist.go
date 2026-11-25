package trust

import (
	"errors"
	"fmt"
	"strconv"
)

// A macOS access list is judged here on every system, so the tests of its every rule run on every
// system; it is read only on darwin. The values are <sys/kauth.h>'s.
const (
	macKindMask    = 0xf    // KAUTH_ACE_KINDMASK
	macPermit      = 1      // KAUTH_ACE_PERMIT
	macDeny        = 2      // KAUTH_ACE_DENY
	macOnlyInherit = 1 << 8 // KAUTH_ACE_ONLY_INHERIT: the entry is for what is made in the folder

	// macWrite is every right that changes a name: WRITE_DATA (ADD_FILE), DELETE, APPEND_DATA
	// (ADD_SUBDIRECTORY), DELETE_CHILD, WRITE_ATTRIBUTES, WRITE_EXTATTRIBUTES, WRITE_SECURITY,
	// CHANGE_OWNER, GENERIC_ALL and GENERIC_WRITE.
	macWrite = 1<<2 | 1<<4 | 1<<5 | 1<<6 | 1<<8 | 1<<10 | 1<<12 | 1<<13 | 1<<21 | 1<<23
	// macRead is every right that reads a file's content: READ_DATA, GENERIC_ALL and GENERIC_READ.
	macRead = 1<<1 | 1<<21 | 1<<24
	// macSearch is every right that reaches into a folder: SEARCH (EXECUTE) and GENERIC_EXECUTE. Listing
	// a folder is READ_DATA, which macRead holds.
	macSearch = 1<<3 | 1<<22
)

// macACE is one entry of a macOS access list: whom it applies to, "uid:<n>" or "gid:<n>" when
// its GUID is the kernel's form of an account or group id, and "guid:<GUID>" otherwise; its flags
// and its rights.
type macACE struct {
	who           string
	flags, rights uint32
}

// judgeMac is the rule of a macOS access list: a list that was not read is refused, matching
// ErrOthersWrite, as it may let another account write the name, and errors.ErrUnsupported. An
// entry neither allow nor deny is refused, and so is an allow entry, not inherit-only, or inherit-only
// and inherit, as judgeEntries's, that skip does not pass and that gives a right of macWrite, or
// one of reads, the rights another account may not have to read the name: macRead for a key or a
// private file, macRead and macSearch for a private folder. The fix revokes that entry's account
// or group on the name the walk reached, a link's own list for a link: its entries come off, all
// but its deny entries, which keeps those that guard a home folder.
func judgeMac(n node, path string, reads uint32, inherit bool, skip func(macACE) bool) error {
	if n.macUnread != "" {
		return refuse(path, path+" may hold an access list that is not read here: "+n.macUnread, Fix{}, fmt.Errorf("%w: %w", ErrOthersWrite, errors.ErrUnsupported))
	}
	if n.nfsUnshown && reads != 0 {
		return refuse(path, path+" is on an NFS mount that cannot show the server's access list, which may let another account read it", Fix{}, fmt.Errorf("%w: %w", ErrOthersRead, errors.ErrUnsupported))
	}
	write, read := uint32(macWrite), uint32(macRead)
	if n.nfs4 {
		write, read, reads = nfsWrite, nfsRead, nfsReads(reads)
		if !n.typ.IsDir() {
			read |= nfsSearch // EXECUTE on a file reads it at the server (RFC 8881, 6.2.1.3.1)
		}
	}
	for _, e := range n.mac {
		fix, w := Fix{Path: path, Revoke: e.who}, write
		if n.nfs4 {
			w &^= nfsModeWrite(n, e)
		}
		switch kind := e.flags & macKindMask; {
		case kind != macPermit && kind != macDeny:
			return refuse(path, path+" has an access entry of kind "+strconv.Itoa(int(kind))+", which is not read here", fix, ErrOthersWrite)
		case kind == macDeny || e.flags&macOnlyInherit != 0 && !inherit || skip(e):
		case e.rights&w != 0:
			return refuse(path, path+" has an access list that lets "+e.who+" change it, with the rights "+hex(e.rights), fix, ErrOthersWrite)
		case e.rights&reads&read != 0:
			return refuse(path, path+" has an access list that lets "+e.who+" read it, with the rights "+hex(e.rights), fix, ErrOthersRead)
		case e.rights&reads != 0:
			return refuse(path, path+" has an access list that lets "+e.who+" search it, with the rights "+hex(e.rights), fix, ErrOthersRead)
		}
	}
	return nil
}

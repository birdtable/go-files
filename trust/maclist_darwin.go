package trust

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	// macAttrs is what getattrlist is asked of every name: which attributes the answer holds, the
	// device, the access list (absent when none is set) and the inode.
	macAttrs = unix.ATTR_CMN_RETURNED_ATTRS | unix.ATTR_CMN_DEVID | unix.ATTR_CMN_EXTENDED_SECURITY | unix.ATTR_CMN_FILEID

	filesecMagic = 0x012cc16d // KAUTH_FILESEC_MAGIC
	filesecNoACL = 0xffffffff // KAUTH_FILESEC_NOACL: a filesec that holds no list
	filesecHead  = 44         // the magic, the owner's and group's GUIDs, the entry count and flags
	macACESize   = 24         // an entry: the GUID it applies to, its flags and its rights

	// macBuf is the bytes an answer is read into: the header, the three attributes and a filesec
	// of KAUTH_ACL_MAX_ENTRIES (128) entries take 3,160. An answer longer than the buffer is
	// refused, never cut short.
	macBuf = 8192

	// The first 12 bytes of the GUID the kernel gives an account, or a group, with no GUID of its
	// own: the id is the last 4, high byte first. Root's is FFFFEEEE-DDDD-CCCC-BBBB-AAAA00000000.
	userGUID  = "\xff\xff\xee\xee\xdd\xdd\xcc\xcc\xbb\xbb\xaa\xaa"
	groupGUID = "\xab\xcd\xef\xab\xcd\xef\xab\xcd\xef\xab\xcd\xef"
)

// listOf is n, the facts of the name path whose lstat or fstat is info, with its access list:
// read by getattrlist, never following a link, and held to the same file by its device and inode,
// so a name swapped between the two reads is errMoved, read again by the caller, and ReadOwned's
// list is its descriptor's file's.
func listOf(n node, path string, info fs.FileInfo) (node, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return node{}, &fs.PathError{Op: "stat", Path: path, Err: errors.New("this system's stat gives no device")}
	}
	buf := make([]byte, macBuf)
	if err := getattrlist(path, buf); err != nil {
		return node{}, err
	}
	return listed(n, buf, uint32(st.Dev), st.Ino) //nolint:gosec // G115: dev_t is 32 bits, and the answer's is read as its bits
}

// getattrlist reads macAttrs of the one name path, following no link, into buf. It is this
// module's one raw system call: x/sys has no wrapper for getattrlist(2) yet
// (golang/go#83151), and when it gains one this call and its lint exclusion go.
func getattrlist(path string, buf []byte) error {
	p, err := unix.BytePtrFromString(path)
	if err != nil {
		return &fs.PathError{Op: "getattrlist", Path: path, Err: err}
	}
	al := unix.Attrlist{Bitmapcount: unix.ATTR_BIT_MAP_COUNT, Commonattr: macAttrs}
	//nolint:gosec,staticcheck // G103: the name, the attributes asked and the buffer, each by address for the one call, as getattrlist(2) takes them; SA1019: libSystem's getattrlist has no wrapper in x/sys (golang/go#83151)
	_, _, e := unix.Syscall6(unix.SYS_GETATTRLIST, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&al)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), unix.FSOPT_NOFOLLOW, 0)
	if e != 0 {
		return &fs.PathError{Op: "getattrlist", Path: path, Err: e}
	}
	return nil
}

// listed is n with the access list of ans, getattrlist's answer to macAttrs for a name whose stat
// gave the device dev and the inode ino. An answer that cannot be read leaves the list unread, and
// judgeMac refuses it. One for another file, as when the name was replaced by rename after the
// lstat, or after ReadOwned's open, is errMoved, and the name is read again from the first.
func listed(n node, ans []byte, dev uint32, ino uint64) (node, error) {
	adev, aino, aces, err := parseAnswer(ans)
	switch {
	case err != nil:
		n.macUnread = err.Error()
	case adev != dev || aino != ino:
		return node{}, errMoved
	default:
		n.mac = aces
	}
	return n, nil
}

// parseAnswer reads getattrlist's answer to macAttrs: its length, the attributes it holds, the
// device, the reference to the filesec when a list is set, and the inode, each 4-byte aligned in
// the machine's order, little-endian on every Mac. Anything else it holds, or a reference out of
// it, is an error.
func parseAnswer(b []byte) (dev uint32, ino uint64, aces []macACE, err error) {
	le := binary.LittleEndian
	if len(b) < 24 {
		return 0, 0, nil, errors.New("the answer is shorter than its header")
	}
	total := le.Uint32(b)
	if total < 24 || uint64(total) > uint64(len(b)) {
		return 0, 0, nil, fmt.Errorf("the answer says it holds %d bytes, of %d read", total, len(b))
	}
	b = b[:total]
	got := le.Uint32(b[4:])
	const must = unix.ATTR_CMN_DEVID | unix.ATTR_CMN_FILEID
	if got&^macAttrs != 0 || got&must != must ||
		le.Uint32(b[8:]) != 0 || le.Uint32(b[12:]) != 0 || le.Uint32(b[16:]) != 0 || le.Uint32(b[20:]) != 0 {
		return 0, 0, nil, fmt.Errorf("the answer holds the attributes %#x, %#x, %#x, %#x, %#x, not those asked",
			got, le.Uint32(b[8:]), le.Uint32(b[12:]), le.Uint32(b[16:]), le.Uint32(b[20:]))
	}
	listed := got&unix.ATTR_CMN_EXTENDED_SECURITY != 0
	end := 24 + 4 + 8
	if listed {
		end += 8
	}
	if len(b) < end {
		return 0, 0, nil, errors.New("the answer ends before its attributes")
	}
	off := 24
	dev, off = le.Uint32(b[off:]), off+4
	if listed {
		// An attrreference: the list's offset from the reference itself, and its length.
		start, size := uint64(off)+uint64(le.Uint32(b[off:])), uint64(le.Uint32(b[off+4:]))
		off += 8
		if start < uint64(end) || start+size > uint64(len(b)) {
			return 0, 0, nil, fmt.Errorf("the access list's %d bytes at %d lie outside the answer", size, start)
		}
		if aces, err = parseFilesec(b[start : start+size]); err != nil {
			return 0, 0, nil, err
		}
	}
	return dev, le.Uint64(b[off:]), aces, nil
}

// parseFilesec reads a kauth_filesec: its magic, two GUIDs it is not judged by, the count of its
// entries and its flags, then each entry. A filesec that says it holds no list has no entries.
// Bytes past the last entry, or too few for the count, are an error.
func parseFilesec(b []byte) ([]macACE, error) {
	le := binary.LittleEndian
	if len(b) < filesecHead {
		return nil, fmt.Errorf("the access list is %d bytes, shorter than its header", len(b))
	}
	if m := le.Uint32(b); m != filesecMagic {
		return nil, fmt.Errorf("the access list starts %#x, not the magic %#x", m, filesecMagic)
	}
	count := uint64(le.Uint32(b[36:]))
	if count == filesecNoACL {
		count = 0
	}
	if uint64(len(b)) != filesecHead+count*macACESize {
		return nil, fmt.Errorf("the access list is %d bytes, and %d entries take %d", len(b), count, filesecHead+count*macACESize)
	}
	aces := make([]macACE, 0, count)
	for e := b[filesecHead:]; len(e) > 0; e = e[macACESize:] {
		aces = append(aces, macACE{who: whoOf(e[:16]), flags: le.Uint32(e[16:]), rights: le.Uint32(e[20:])})
	}
	return aces, nil
}

// whoOf names the 16-byte GUID g. Only an account's id form names an account the walk can trust:
// an account's own GUID, from the directory, cannot be mapped to its id without libc, and is
// never trusted.
func whoOf(g []byte) string {
	switch string(g[:12]) {
	case userGUID:
		return "uid:" + strconv.FormatUint(uint64(binary.BigEndian.Uint32(g[12:])), 10)
	case groupGUID:
		return "gid:" + strconv.FormatUint(uint64(binary.BigEndian.Uint32(g[12:])), 10)
	}
	return fmt.Sprintf("guid:%X-%X-%X-%X-%X", g[0:4], g[4:6], g[6:8], g[8:10], g[10:16])
}

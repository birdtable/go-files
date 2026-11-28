// Package trust asks one question of a path before a program runs or reads what it names: may
// this program believe it, given who owns it and who can write it? Path walks the path from the
// top of its volume down, one name at a time, read without following it. ReadOwned reads a whole
// file through one descriptor checked to be plain, owned and small. Held judges a file or folder
// already open, on its descriptor, as Path judges the last name. MkdirPrivate makes a folder only
// this account reaches, judged before it is made and again once it is there. Every rule is
// written once, here, and judges what a host read; the README says each one, per system.
//
// On macOS, Path and ReadOwned read each name's access list as well as its mode bits, and refuse a
// list that lets another account write it, or one they cannot read. On FreeBSD,
// NetBSD and every other Unix, whose lists are not read, they refuse every name.
//
// On Windows a path that names a device or a pipe, not a path on a drive or a volume (\\.\, or
// \\?\ or \??\ naming neither a drive nor a Volume{GUID}), is refused before anything is opened.
// Each open trust makes of a name before it has judged the folders on the way asks for
// identification alone, so a pipe a link leads to learns who the caller is and may act as it in
// nothing; an open made after the judgement is of folders no other account may change. A UNC path
// (\\host\share\...) is not refused: a share is a place for files, and an open of one connects to
// the named host and authenticates the caller to it. A program that takes paths from another
// account refuses UNC paths itself, where filepath.VolumeName starts with \\.
//
// Built on the rule of git's safe.directory and OpenSSH's StrictModes, every folder from the top
// down root's or this account's and writable by no other, read through the standard library and
// golang.org/x/sys. It adds that rule written once for every system, the access lists of Linux,
// macOS and Windows, and the fix as data. The program configures whom it trusts (Who: RootOnly,
// NoLinks, Group, Apart), the Kind of each path, and ReadOwned's owner, privacy and limit.
//
// It imports the standard library, golang.org/x/sys, confine, internal/fileid and internal/procfs.
package trust

import (
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/birdtable/go-files/internal/fileid"
)

// Kind is what the last name of a path must be. Every name before it must be a folder or a link.
type Kind int

// The kinds of the last name.
const (
	// File is a regular file that is read and not run.
	File Kind = iota
	// Folder is a folder.
	Folder
	// Key is a regular file only its owner may read or write.
	Key
	// Program is a regular file its owner may execute.
	Program
	// Socket is a Unix socket.
	Socket
	// PrivateFolder is a folder only its owner may read or write, judged as Key is: by its mode,
	// Linux access list mask, macOS access list and Windows access list.
	PrivateFolder
)

// Who is whom the walk trusts. Root, or on Windows Administrators, SYSTEM and TrustedInstaller, is
// always trusted; so is the account that runs the program, unless RootOnly.
type Who struct {
	// RootOnly trusts root alone: not this account, no group, no sticky folder and no link.
	RootOnly bool
	// NoLinks refuses a link anywhere on the path, the last name included.
	NoLinks bool
	// Group, when it answers true for a name's group, lets that group write the name, as a package
	// manager's folder its group shares. Nil lets no group write. Windows has no group to ask.
	Group func(gid uint32) bool
	// Overflow trusts the kernel's overflow account as root, for a test in a Linux user namespace that
	// maps this account and not root. Path, Held and ReadOwned refuse it with ErrOverflow, walking
	// and reading nothing, anywhere else. Those refusals say "the overflow
	// account setting".
	Overflow bool
	// Apart is folders the path may not be, nor lie below, each absolute and clean, read once
	// through its links as what it names. A name on the way that is the same file as one of them,
	// by its file ID, is refused with ErrInside, whatever spelling reached it. The
	// refusals say "a folder named to be kept apart", and "the folders named to be kept apart hold".
	Apart []string
	// FileSystems, on Linux, adds file system types, by statfs's f_type, as erofs's 0xe0f5e1e2, whose
	// names pass on the owners they report. A 9p refusal says "no file system type
	// named as trusted is 9p's".
	FileSystems []uint32
	// Mounts, on Linux, is absolute folders where a name on a disk mounted nosuid, as a hardened
	// /home, or noexec without nosuid, as usbmount mounts a stick, passes when on the same device.
	// Path, Held and ReadOwned judge each entry at every call, before
	// the path, and refuse the call with ErrMountEntry, the entry as Unsafe.Path, unless it is
	// absolute and clean, every name above it is one only root may change (as a walk with RootOnly
	// judges its parent), it is no link, and it is the root of a mount: on Linux by its mount,
	// statx's STATX_ATTR_MOUNT_ROOT, refused on a kernel before 5.8, which has none, so a disk bound
	// over a folder on the same disk counts; on macOS and every other Unix by a device other than
	// its parent's. So an entry at a name another account controls, as a link in
	// /tmp, or one whose disk is not mounted, which names the disk beneath, is refused. The check
	// holds the entry's name, one only root can move, and that a mount is there; not on whose ask
	// root mounted it, nor whose disk it is. A mount point a root helper fills for whoever plugs a
	// disk in, as usbmount's /media/usb0 or udisks' shared /media/<label>, passes, and lends its
	// pass to that disk, whose owners and modes its maker wrote: name one only where every account
	// that can fill it is trusted with what the pass lets through. The entry's owner is not judged: an account may own the folder root mounted for it, as
	// /run/user/<uid>. In a Linux user namespace that maps this account and not root, the names
	// above an entry are root's as the overflow account, and pass with Overflow. Windows reads none.
	// The refusals say "the mounts named as trusted hold" an entry, or "on no mount named as trusted".
	Mounts []string
}

// The refusals, each its own, so a program says which it found.
var (
	// ErrNotPlain is a name that is not of its kind: a folder where a file is asked, a pipe, a
	// device, or a link where none is followed.
	ErrNotPlain = errors.New("is not of the kind asked for")
	// ErrNotOwners is a name an account the walk does not trust owns.
	ErrNotOwners = errors.New("belongs to an account that is not trusted")
	// ErrOthersWrite is a name its group or another account can write.
	ErrOthersWrite = errors.New("can be written by its group or by others")
	// ErrOthersRead is a key, or a private file, its group or another account can read.
	ErrOthersRead = errors.New("can be read by its group or by others")
	// ErrTooLong is a file over its limit, refused whole and never cut short.
	ErrTooLong = errors.New("holds more than its limit")
	// ErrOverflow is Who.Overflow where Path, Held and ReadOwned refuse it, or a program run as the
	// overflow account. Its refusals of Who.Overflow say "the overflow account setting".
	ErrOverflow = errors.New("is refused for the kernel's overflow account")
	// ErrReplaced is a name that named another file, or none, at each of the tries a check takes:
	// on macOS, whose access list is read by the name and held to the file judged by its device and
	// inode, a file its owner keeps replacing by rename while it is checked, or a file Held holds
	// whose name was moved away. Nothing about the file was judged, and another call may pass.
	ErrReplaced = errors.New("was replaced by another file at each try of its check")
	// ErrInside is a path that is, or lies below, a folder of Who.Apart: a name on its way is that
	// folder by its file ID. Its refusals say "a folder named to be kept apart".
	ErrInside = errors.New("is a folder the walk is kept apart from, or lies below one")
	// ErrOwnersIgnored is a name on a macOS volume mounted to ignore ownership, as an external
	// disk usually is: every account is shown there as the owner of every name, with the owner's
	// rights, so no owner can be told. It matches ErrNotOwners too.
	ErrOwnersIgnored = fmt.Errorf("is on a volume that ignores ownership: %w", ErrNotOwners)
	// ErrMount is a name on a Linux mount whose owners trust does not judge, as CIFS or 9p, or a
	// disk mounted nosuid, or noexec without nosuid, as an account's stick is. It matches
	// ErrNotOwners too. Who.Mounts stands behind "on no
	// mount named as trusted", and Who.FileSystems behind "no file system type named as trusted".
	ErrMount = fmt.Errorf("is on a mount whose owners are not judged: %w", ErrNotOwners)
	// ErrMountEntry is a folder of Who.Mounts that is not a mount's root at a name only root can
	// move: not absolute and clean, below a name an account but root may change, a link, no mount's
	// root, or naming nothing. The call is refused before its path is walked. A folder that passes
	// is not vouched for by whose ask it was mounted on, as Who.Mounts says. It matches ErrMount
	// too. Its refusals say "the mounts named as trusted hold" the entry.
	ErrMountEntry = fmt.Errorf("names as trusted a folder that is not a mount's root at a name only root can move: %w", ErrMount)
)

// errMoved is a host's answer for a name that named another file between two of its reads, read
// again up to tries times before it is refused with ErrReplaced.
var errMoved = errors.New("the name now names another file than the one judged")

// tries is how many times a name is read whole before a swap at it is refused: a file replaced
// by rename between a name's two reads is read again from the first, as a second call would.
const tries = 8

// replaced is the refusal of path, a name that was another file at each of its tries.
func replaced(path string) *Unsafe {
	return refuse(path, path+" was replaced by another file, or moved away, at each of "+strconv.Itoa(tries)+" tries to check it", Fix{}, ErrReplaced)
}

// Unsafe is a name the walk does not trust. Observed says what was seen, and Fix is the change that
// makes the next check pass, as data, or the zero Fix when only another setting can. It matches
// the refusal above it was refused with, when there is one.
type Unsafe struct {
	// Path is the name the refusal is about: the path asked for, or a folder on its way, link or
	// name, as the walk reached it. It is "" for a refusal of a setting of Who, which names no path.
	// Observed names it in words; a caller that asks which name was judged compares Path.
	Path     string
	Observed string
	Fix      Fix
	err      error
}

// Fix is a change to one name that makes the next check of it pass, as data for the caller to show
// or make in its own words, or as the argv of each command that makes it (Commands): nothing in
// trust quotes or joins a command line. Each field that is set is one
// part of the change. The zero Fix is none, when only another setting can make the check pass:
// another kind asked for, another Who, or the name moved.
type Fix struct {
	// Path is the name to change, as the walk reached it: a link is the link itself, and never
	// what it leads to.
	Path string
	// Clear is the mode bits to take away, and Set the ones to add (Unix).
	Clear, Set fs.FileMode
	// Owner is the account to give the name to, in Account's form. The walk's fixes name
	// "uid:0", root, on Unix, and S-1-5-32-544, the Administrators group, on Windows; a caller's
	// own Fix may name any account, this one among them.
	Owner string
	// Revoke is whose entries come off the name's access list. On Windows it is a SID, and its
	// allow entries come off once the entries the name inherits are copied onto it, so that an
	// inherited one comes off too. On macOS it is uid:<n>, gid:<n> or guid:<GUID>, and its every
	// entry but its deny entries comes off. On Linux, for an NFSv4 list, it is nfs4:<who> or
	// nfs4group:<who>, the principal as the server names it, or uid:<n> or gid:<n> for OWNER@ or
	// GROUP@, and the same entries come off.
	Revoke string
	// Reset replaces a null access list with the entries the name inherits (Windows).
	Reset bool
}

func (e *Unsafe) Error() string { return e.Observed }

func (e *Unsafe) Unwrap() error { return e.err }

const maxLinks = 40

// administrators is the Administrators group's SID, an owner Windows always trusts and the one a
// Fix gives a name to there.
const administrators = "S-1-5-32-544"

// creatorOwner is CREATOR OWNER, which an inherit-only entry names to give whoever makes a name in
// the folder the rights on it: on the new name it becomes the maker's own entry.
const creatorOwner = "S-1-3-0"

// Path walks path, which must be absolute and clean, from the top of its volume down, and answers
// it with no link left in it when every name on the way passes for who and the last is of the
// kind last. A name that is not there, or that cannot be read, is an error and never a pass. A
// name replaced by another file while it is read is read again, and refused with ErrReplaced when
// it is replaced at each try.
func Path(path string, last Kind, who Who) (string, error) { return walk(path, last, who, local()) }

// Account is this account as ReadOwned takes an owner and the walk trusts it, "uid:N" on Unix and
// the SID on Windows, with the walk's own refusal where it judges nothing: "" where the SID cannot
// be read, and on Linux the account read beside ErrOverflow, which ReadOwned refuses for any owner.
func Account() (string, error)       { return account(local()) }
func account(h host) (string, error) { return h.me, h.broken }

// OwnerOf is the owner of the open file or folder f, read on its handle and never by a name, in the
// form ReadOwned takes and Account gives: "uid:N" on Unix and the SID on Windows. A folder's owner
// is read by opening it first, root.Open(".") for a root, so a folder swapped in at the name after
// the open is not the one read. It judges nothing: whether that owner is trusted is
// the caller's, as Path and ReadOwned judge it.
func OwnerOf(f *os.File) (string, error) {
	owner, err := ownerOf(f)
	if err == nil && !ownerForm.MatchString(owner) {
		err = fmt.Errorf("%q is not an owner, as uid:1000 or a SID", owner)
	}
	if err != nil {
		return "", fmt.Errorf("the owner of %s: %w", f.Name(), err)
	}
	return owner, nil
}

// host is what the walk and ReadOwned ask of the system; a test hands its own. root is the owners
// always trusted, me the account that runs the program, and broken, when set, why this host can
// judge nothing (on Windows, this account's SID could not be read), answered before any name.
type host struct {
	look     func(path string) (node, error)
	ident    func(path string) (fileID, error)
	readlink func(path string) (string, error)
	open     func(path string) (*os.File, error)
	stat     func(f *os.File) (node, error)
	// mask is whether the file held has a Linux access list, read on its descriptor, for Held;
	// nil where none shows in the mode, and Held then takes a group-writable name to have one.
	mask func(f *os.File) (bool, error)
	// mountRoot is whether entry, a name that is no link, is the root of a mount, parent being the
	// folder above it, for Who.Mounts; nil on Windows, which judges no entry.
	mountRoot func(entry, parent string) (bool, error)
	root      []string
	me        string
	broken    error
	overflow  string
	mounts    Who // ReadOwned's, of which only FileSystems and Mounts are read
}

// node is what the walk reads of one name, without following it. typ is its type bits, 0 for a
// regular file. Unix fills perm (with the sticky bit), gid and acl, whether a group-writable name
// holds a Linux access list, and macOS mac, the entries of its access list, or macUnread, why
// that list could not be read, as on another Unix, and Linux mac with an NFSv4 list (nfs4);
// Windows fills dacl in their place.
type node struct {
	id        fileID
	typ       fs.FileMode
	owner     string
	perm      fs.FileMode
	gid       uint32
	acl       bool
	mac       []macACE
	macUnread string
	// nfs4 is whether mac holds the NFSv4 list of a name on a Linux NFS mount, in NFSv4's rights.
	nfs4 bool
	// nfsUnshown is whether the name is on a Linux NFS mount that cannot show the server's list, as
	// NFSv3, so a private kind is refused.
	nfsUnshown bool
	// ownersIgnored is the folder the name's volume is mounted on, when that volume ignores
	// ownership (macOS), and "" otherwise.
	ownersIgnored string
	// mounter is the account that mounted the name's volume, written as an owner is, where the
	// system tells it: every volume on macOS, a FUSE file system on Linux; mountedOn is the folder
	// it is mounted on. Both are "" where nothing is told.
	mounter, mountedOn string
	// fsType is the statfs type of the name's Linux file system, 0 elsewhere; nosuid and noexec, its
	// mount's, by fstatfs; typed, whether fstatfs read it; kind, the
	// type mountinfo gives its mount, read for every type not passed by its device, and "" where it
	// is not read; anon, for a type passed by its device, whether that device's major is 0, as a 9p
	// mount's is.
	fsType uint32
	nosuid bool
	noexec bool
	typed  bool
	anon   bool
	kind   string
	// zfsList is a ZFS name carrying system.nfs4_acl_xdr, a list trust does not read.
	zfsList bool
	dacl    *dacl
	size    int64
}

// fileID is a file's identity on its volume: on Unix its device and inode, and on Windows its
// volume's serial number and its file index. Two names are one file when theirs are
// equal, however each is spelled.
type fileID struct{ vol, idx uint64 }

// apartee is a folder of Who.Apart, as it was given and by the file it names.
type apartee struct {
	path string
	id   fileID
}

type dacl struct {
	null bool
	aces []ace
}

type ace struct {
	typ, flags uint8
	mask       uint32
	sid        string
	sandbox    bool
}

// appContainer is whether a SID of the authority auth and the sub-authorities subs is an
// AppContainer's: authority 15, a first sub-authority of 2 or 3 and one more after it,
// read from the SID's parts, never from its string.
func appContainer(auth [6]byte, subs []uint32) bool {
	return auth == [6]byte{0, 0, 0, 0, 0, 15} && len(subs) > 1 && (subs[0] == 2 || subs[0] == 3)
}

// sidFields reads the SID in b as winnt.h lays it out, and is false when b holds no such SID
// whole. It reads the entry's own bytes, not x/sys's SID methods, which build pointers from what
// Windows answers and which the race detector's pointer checks refuse.
func sidFields(b []byte) (auth [6]byte, subs []uint32, ok bool) {
	if len(b) < 8 || b[0] != 1 || b[1] > 15 || len(b) < 8+4*int(b[1]) {
		return auth, nil, false
	}
	copy(auth[:], b[2:8])
	subs = make([]uint32, b[1])
	for i := range subs {
		subs[i] = binary.LittleEndian.Uint32(b[8+4*i:])
	}
	return auth, subs, true
}

// sidString writes a SID as ConvertSidToStringSid does: S-1-, the authority in decimal, or in hex
// as 0x and twelve digits when it is 2^32 or more, then each sub-authority in decimal.
func sidString(auth [6]byte, subs []uint32) string {
	var n uint64
	for _, x := range auth {
		n = n<<8 | uint64(x)
	}
	s := "S-1-" + strconv.FormatUint(n, 10)
	if n >= 1<<32 {
		s = fmt.Sprintf("S-1-0x%012X", n)
	}
	for _, x := range subs {
		s += "-" + strconv.FormatUint(uint64(x), 10)
	}
	return s
}

// The parts of a Windows access entry the rules read, as winnt.h numbers them. writeClass is
// every right that changes a name or what it holds (rule 2), and readClass every right that reads
// what a file holds.
const (
	aceAllow, aceDeny = 0, 1
	inheritOnly       = 0x8
	addSubdirectory   = 0x4
	writeClass        = 0x2 | 0x4 | 0x10 | 0x40 | 0x100 | 0x10000 | 0x40000 | 0x80000 | 0x2000000 | 0x10000000 | 0x40000000
	readClass         = 0x1 | 0x2000000 | 0x10000000 | 0x80000000
)

func walk(path string, last Kind, who Who, h host) (string, error) {
	h = overflow(h, who.Overflow)
	if h.broken != nil {
		return "", h.broken
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", &Unsafe{Path: path, Observed: strconv.Quote(path) + " is not an absolute, clean path"}
	}
	if err := device(path); err != nil {
		return "", err
	}
	apart, err := apartOf(who.Apart, h)
	if err != nil {
		return "", err
	}
	if err := mountEntries(who.Mounts, who.FileSystems, h); err != nil {
		return "", err
	}
	return walkJudged(path, last, who, h, apart)
}

// walkJudged is walk once h has taken who.Overflow, path is absolute and clean, apart is who.Apart
// read, and each folder of who.Mounts is judged.
func walkJudged(path string, last Kind, who Who, h host, apart []apartee) (string, error) {
	rest := path
	for range maxLinks + 1 {
		got, again, err := pass(rest, last, who, h, apart)
		if err != nil || again == "" {
			return got, err
		}
		if err := device(again); err != nil {
			return "", err
		}
		rest = again
	}
	return "", &Unsafe{Path: path, Observed: path + " passes more than " + strconv.Itoa(maxLinks) + " links"}
}

// apartOf reads each folder of paths once, through its links, as what it names. One that is not
// absolute and clean is refused, and one that cannot be read is an error, never a pass.
func apartOf(paths []string, h host) ([]apartee, error) {
	apart := make([]apartee, 0, len(paths))
	for _, p := range paths {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return nil, &Unsafe{Observed: "the folders named to be kept apart hold " + strconv.Quote(p) + ", which is not an absolute, clean path"}
		}
		if err := device(p); err != nil {
			return nil, err
		}
		id, err := h.ident(p)
		if err != nil {
			return nil, err
		}
		apart = append(apart, apartee{path: p, id: id})
	}
	return apart, nil
}

// pass walks rest down to its first link, and answers the path it reached, or, at a link once it
// is judged, the path to walk again: the link's target, then the names after it. A name that is a
// folder of apart is refused, by the file ID its own read gave. A . in rest, or an empty name, is
// the folder the walk is in, and a .. the folder above it, each read and judged again where it
// is, so a .. after a link goes up from the link's target, as the system goes.
func pass(rest string, last Kind, who Who, h host, apart []apartee) (got, again string, err error) {
	steps := split(rest)
	var way []reached
	for i, step := range steps {
		next, from, back := enter(way, i, step)
		way = back
		n, err := lookWhole(next, h)
		if err != nil {
			return "", "", err
		}
		if i := slices.IndexFunc(apart, func(a apartee) bool { return a.id == n.id }); i >= 0 {
			return "", "", refuse(next, next+" is "+apart[i].path+", a folder named to be kept apart, so the path is in it", Fix{}, ErrInside)
		}
		at := place{path: next, last: i == len(steps)-1, in: from.in, dev: from.dev}
		if n.typ == fs.ModeSymlink {
			target, err := follow(n, at, who, h)
			if err != nil {
				return "", "", err
			}
			return "", spell(target, steps[i+1:]...), nil
		}
		k := Folder
		if at.last {
			k = last
		}
		if err := judge(n, k, at, who, h); err != nil {
			return "", "", err
		}
		way = append(way, reached{path: next, in: opened(n, h), dev: n.id.vol})
	}
	return way[len(way)-1].path, "", nil
}

// reached is a folder the walk passed: its path, with no link in it, whether opened answers true
// for it, and its device, which a name right in it is judged by.
type reached struct {
	path string
	in   bool
	dev  uint64
}

// enter is where step, the i-th name of the walk, leads from way, the folders it passed: the path
// to read, the folder that holds it, and way less each folder a . or a .. takes the walk back out
// of. A .. at the top stays at the top.
func enter(way []reached, i int, step string) (string, reached, []reached) {
	switch {
	case i == 0:
		return step, reached{}, way
	case step == "" || step == "." || step == "..":
		if step == ".." && len(way) > 1 {
			way = way[:len(way)-1]
		}
		next := way[len(way)-1].path
		way = way[:len(way)-1]
		from := reached{}
		if len(way) > 0 {
			from = way[len(way)-1]
		}
		return next, from, way
	}
	return filepath.Join(way[len(way)-1].path, step), way[len(way)-1], way
}

// spell is first, then each name of rest below it. Where the system takes a .. by its spelling, as
// Windows does, it is filepath.Join; elsewhere nothing is cleaned, as a .. after a link goes up
// from the link's target, which only the walk reads.
func spell(first string, rest ...string) string {
	if bySpelling {
		return filepath.Join(append([]string{first}, rest...)...)
	}
	for _, name := range rest {
		if !strings.HasSuffix(first, string(filepath.Separator)) {
			first += string(filepath.Separator)
		}
		first += name
	}
	return first
}

// lookWhole is h.look of path, read again while the name names another file between its reads,
// and refused with ErrReplaced when it does at every try.
func lookWhole(path string, h host) (node, error) {
	for range tries {
		n, err := h.look(path)
		if !errors.Is(err, errMoved) {
			return n, err
		}
	}
	return node{}, replaced(path)
}

func split(path string) []string {
	top := filepath.VolumeName(path) + string(filepath.Separator)
	rest := strings.TrimPrefix(path, top)
	if rest == "" {
		return []string{top}
	}
	return append([]string{top}, strings.Split(rest, string(filepath.Separator))...)
}

// place is where a name is: its path, whether it is the last name, whether it is right in a
// folder opened answers true for, and the device of the folder it is right in.
type place struct {
	path     string
	last, in bool
	dev      uint64
}

// follow judges the link n and answers its target, absolute, read only once the link passes. On
// Windows the link's own list is judged as a folder's is, but with no pass for adding a folder:
// whoever may change a reparse point may point it elsewhere (rule 5).
func follow(n node, at place, who Who, h host) (string, error) {
	if who.NoLinks || who.RootOnly {
		return "", refuse(at.path, at.path+" is a link, and no link is followed here", Fix{}, ErrNotPlain)
	}
	if err := owned(n, at, who, h); err != nil {
		return "", err
	}
	if n.dacl != nil {
		err := judgeEntries(n.dacl, at.path, false, false, func(e ace) bool { return e.sandbox || trusted(e.sid, who, h) })
		if err != nil {
			return "", err
		}
	}
	if err := judgeMac(n, at.path, 0, false, func(e macACE) bool { return trusted(e.who, who, h) }); err != nil {
		return "", err
	}
	target, err := h.readlink(at.path)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(at.path)
	switch {
	case filepath.IsAbs(target):
		return target, nil
	case strings.HasPrefix(target, string(filepath.Separator)): // rooted, with no volume, on Windows
		return filepath.VolumeName(dir) + target, nil
	}
	return spell(dir, target), nil
}

// refuse is the refusal of path, the name it is about, or of a setting of Who when path is "".
func refuse(path, observed string, fix Fix, err error) *Unsafe {
	return &Unsafe{Path: path, Observed: observed, Fix: fix, err: err}
}

// device refuses path where Windows reads it as a device or a pipe, not a path on a drive or a
// volume: a \\.\ path, or a \\?\ one that names neither a drive nor a Volume{GUID}, as
// fileid.DevicePath reads it, before anything opens it, so no open of trust's lands on a pipe whose
// server learns who the caller is. A UNC path is not refused: it names a share, which an open
// connects to, authenticating the caller to its host. Elsewhere it refuses nothing.
func device(path string) error {
	if fileid.DevicePath(path) {
		return refuse(path, path+" names a device or a pipe, not a path on a drive or a volume", Fix{}, ErrNotPlain)
	}
	return nil
}

// trusted is whether owner is root's, or, unless who.RootOnly, this account's. An owner that is
// "", which no account is, is never trusted, whatever the host holds.
func trusted(owner string, who Who, h host) bool {
	return owner != "" && (slices.Contains(h.root, owner) || !who.RootOnly && owner == h.me)
}

// overflow trusts h.overflow as root for Who.Overflow, in a test on Linux in a namespace mapping no
// root. Off Linux is asked first, so a program there is refused with errors.ErrUnsupported too.
func overflow(h host, on bool) host {
	switch {
	case !on || h.broken != nil:
	case h.overflow == "":
		h.broken = refuse("", "the overflow account setting is taken on Linux alone", Fix{}, fmt.Errorf("%w: %w", ErrOverflow, errors.ErrUnsupported))
	case !testing.Testing():
		h.broken = refuse("", "the overflow account setting is set outside a test binary, where no program may set it", Fix{}, ErrOverflow)
	case !h.noRoot(readProc("/proc/self/uid_map")):
		h.broken = refuse("", "the overflow account setting is set where /proc/self/uid_map cannot be read, maps root, maps the overflow account or maps none", Fix{}, ErrOverflow)
	default:
		h.root = append(slices.Clip(h.root), h.overflow)
	}
	return h
}

// noRoot is whether the uid_map b, read with err, holds lines of three numbers, none whose range
// holds id 0 or h's overflow id, so the overflow account is only every account the map leaves out.
func (h host) noRoot(b []byte, err error) bool {
	var o uint32
	_, bad := fmt.Sscanf(h.overflow, "uid:%d", &o)
	return cmp.Or(err, bad) == nil && !slices.ContainsFunc(strings.Split(strings.TrimSpace(string(b)), "\n"), func(l string) bool {
		var in, out, n uint32
		_, err := fmt.Sscanf(l, "%d %d %d", &in, &out, &n)
		return err != nil || in == 0 || in <= o && o-in < n
	})
}

// overflowOf reads the overflow account from the file uid, with read, and answers why a host whose
// account is me judges nothing: the file cannot be read, or me is that account where
// /proc/self/uid_map is not the one line mapping every id as itself, so me may be any account the
// namespace does not map. Where it is that line, as in the first namespace, me is nobody, served.
func overflowOf(me, uid string, read func(string) ([]byte, error)) (string, error) {
	var n uint32
	b, err := read(uid)
	_, scan := fmt.Sscan(string(b), &n)
	o := "uid:" + strconv.FormatUint(uint64(n), 10)
	if err = cmp.Or(err, scan); err != nil {
		return o, refuse("", fmt.Sprint("the overflow account cannot be read from ", uid, ": ", err), Fix{}, ErrOverflow)
	}
	if m, err := read("/proc/self/uid_map"); o == me && (err != nil || !slices.Equal(strings.Fields(string(m)), []string{"0", "0", "4294967295"})) {
		return o, refuse("", "this account is "+o+", the overflow account a user namespace shows for one it does not map", Fix{}, ErrOverflow)
	}
	return o, nil
}

func judge(n node, k Kind, at place, who Who, h host) error {
	if err := ofKind(n, k, at.path); err != nil {
		return err
	}
	if err := owned(n, at, who, h); err != nil {
		return err
	}
	if n.dacl != nil {
		return judgeACL(n, k, at, who, h)
	}
	if err := judgeMode(n, k, at, who, h); err != nil {
		return err
	}
	return judgeMac(n, at.path, macReads(k), k == PrivateFolder, func(e macACE) bool { return trusted(e.who, who, h) })
}

func ofKind(n node, k Kind, path string) error {
	var want fs.FileMode
	switch k {
	case Folder, PrivateFolder:
		want = fs.ModeDir
	case Socket:
		want = fs.ModeSocket
	default:
		want = 0 // a file, a key and a program are regular files
	}
	if n.typ != want {
		return refuse(path, path+" is "+typeName(n.typ)+", not "+typeName(want), Fix{}, ErrNotPlain)
	}
	return nil
}

func typeName(t fs.FileMode) string {
	switch t {
	case 0:
		return "a regular file"
	case fs.ModeDir:
		return "a folder"
	case fs.ModeSocket:
		return "a socket"
	case fs.ModeSymlink:
		return "a link"
	default:
		return "of type " + t.String()
	}
}

// owned refuses n when an account who does not trust owns it, and on Unix a name right in root's
// open sticky folder, as /tmp, where another account can add a hard link to any file or link it
// can see: there anything but a folder is refused whoever owns it, a folder on another device
// than the sticky folder's, and a folder of root's, but for root its own. The device is the
// kernel's, where a FUSE mount on the folder reports any owner and mode, so a folder another
// account serves never passes as this account's. Another account can make no
// folder of root's, and the sticky bit keeps it from renaming or removing one, so for root that
// folder is this account's own, as tmux's /tmp/tmux-<uid> and OpenSSH's StrictModes judge it.
// There those rules come before the owner's, whose Fix, making it root's, is given to root alone,
// so no Fix is one the next check refuses.
func owned(n node, at place, who Who, h host) error {
	if why := plainRefused(n, who, h); why != "" {
		return refuse(at.path, at.path+" is on a file system "+why, Fix{}, ErrMount)
	}
	switch {
	case n.ownersIgnored != "":
		return ignoresOwners(n, at.path)
	case n.mounter != "" && !trusted(n.mounter, who, h):
		return mountedBy(n, at.path)
	case at.in && n.typ != fs.ModeDir:
		return refuse(at.path, at.path+" is right in "+filepath.Dir(at.path)+", where any account can add a hard link to a file or link of root's or of this account's",
			Fix{}, ErrNotOwners)
	case at.in && n.id.vol != at.dev:
		return refuse(at.path, at.path+" is right in "+filepath.Dir(at.path)+", on another device than it: a file system mounted there reports any owner",
			Fix{}, ErrNotOwners)
	case at.in && slices.Contains(h.root, n.owner) && n.owner != h.me:
		return refuse(at.path, at.path+" is root's, right in "+filepath.Dir(at.path)+", where only a folder of this account's passes",
			Fix{}, ErrNotOwners)
	case !trusted(n.owner, who, h):
		fix := chown(n, at.path)
		if at.in && !slices.Contains(h.root, h.me) {
			fix = Fix{} // made root's, it is refused as root's
		}
		return refuse(at.path, at.path+" is owned by "+n.owner+", which is not trusted here", fix, ErrNotOwners)
	}
	return nil
}

// ignoresOwners is the refusal of path, a name on a volume mounted to ignore ownership: only
// another setting fixes it, the volume's, so its Fix is the zero one.
func ignoresOwners(n node, path string) *Unsafe {
	return refuse(path, path+" is on the volume mounted on "+n.ownersIgnored+", which ignores ownership: every account is shown as its owner, with the owner's rights",
		Fix{}, ErrOwnersIgnored)
}

// mountedBy is the refusal of path, a name on a volume an account not trusted mounted: its FUSE
// server, or the image it attached, reports any owner and mode it likes, so nothing read there says
// who can change the name. Only another mount fixes it, so its Fix is the zero one.
func mountedBy(n node, path string) *Unsafe {
	return refuse(path, path+" is on the volume mounted on "+n.mountedOn+" by "+n.mounter+", which is not trusted here: it reports any owner and mode",
		Fix{}, ErrNotOwners)
}

func chown(n node, path string) Fix {
	if n.dacl != nil {
		return Fix{Path: path, Owner: administrators}
	}
	return Fix{Path: path, Owner: "uid:0"}
}

// opened is whether n is a folder its group or others can write that judgeMode passes above the
// last name: root's, with the sticky bit set, as /tmp.
func opened(n node, h host) bool {
	return n.dacl == nil && n.typ == fs.ModeDir && slices.Contains(h.root, n.owner) && n.perm&fs.ModeSticky != 0 && n.perm.Perm()&0o022 != 0
}

// judgeMode is the rule of the mode bits: neither the group nor others may write the name, but for
// root's sticky folder above the last name and a group who.Group lets in; on Linux not even that
// group when the group bits are an access list's mask. A program is one its owner may execute, and
// a key one its group and others can neither read nor write.
func judgeMode(n node, k Kind, at place, who Who, h host) error {
	perm := n.perm.Perm()
	sticky := opened(n, h) && !at.last && !who.RootOnly
	group := !who.RootOnly && who.Group != nil && who.Group(n.gid)
	switch {
	case sticky:
	case perm&0o002 != 0 || (perm&0o020 != 0 && !group):
		return refuse(at.path, at.path+" can be written by "+writers(perm), Fix{Path: at.path, Clear: 0o022}, ErrOthersWrite)
	case perm&0o020 != 0 && n.acl:
		return refuse(at.path, at.path+" can be written by its group bits, which are the mask of an access list that can name any account",
			Fix{Path: at.path, Clear: 0o020}, ErrOthersWrite)
	}
	switch {
	case k == Program && perm&0o100 == 0:
		return refuse(at.path, at.path+" cannot be executed by its owner", Fix{Path: at.path, Set: 0o100}, nil)
	case private(k) && perm&0o077 != 0:
		return refuse(at.path, at.path+" has the mode 0"+strconv.FormatUint(uint64(perm), 8)+", so it can be read or written by its group or others",
			Fix{Path: at.path, Clear: 0o077}, ErrOthersRead)
	}
	return nil
}

func writers(perm fs.FileMode) string {
	switch {
	case perm&0o022 == 0o022:
		return "its group and others"
	case perm&0o020 != 0:
		return "its group"
	}
	return "others"
}

// judgeACL is the rule of a Windows access list, rules 2 to 4 and 6, by judgeEntries. An entry
// passes that is an AppContainer's, a trusted account's, or one that gives only the right
// to add a folder, on a folder above the last name, as a sticky folder does.
func judgeACL(n node, k Kind, at place, who Who, h host) error {
	return judgeEntries(n.dacl, at.path, private(k), k == PrivateFolder, func(e ace) bool {
		addOnly := e.mask&writeClass == addSubdirectory && k == Folder && !at.last
		return e.sandbox || trusted(e.sid, who, h) || addOnly || e.sid == creatorOwner && e.flags&inheritOnly != 0
	})
}

// judgeEntries refuses the list d of the name at path when it is null, holds an entry of a type
// that is neither allow nor deny, or holds an allow entry that is not inherit-only, or is and
// inherit, that skip does not pass, and that gives a right of writeClass, or of readClass when
// read. inherit is for a PrivateFolder: nothing judges the files a program makes in it, which
// start with its inherit-only entries, so those are judged on it.
func judgeEntries(d *dacl, path string, read, inherit bool, skip func(ace) bool) error {
	if d.null {
		return refuse(path, path+" has a null access list, which lets anyone change it", Fix{Path: path, Reset: true}, ErrOthersWrite)
	}
	for _, e := range d.aces {
		switch {
		case e.typ != aceAllow && e.typ != aceDeny:
			return refuse(path, path+" has an access entry of type "+strconv.Itoa(int(e.typ))+", which is not read here", Fix{}, ErrOthersWrite)
		case e.typ == aceDeny || e.flags&inheritOnly != 0 && !inherit || skip(e):
		case e.mask&writeClass != 0:
			return refuse(path, path+" lets "+e.sid+" change it, with the rights "+hex(e.mask), removeGrant(path, e.sid), ErrOthersWrite)
		case read && e.mask&readClass != 0:
			return refuse(path, path+" lets "+e.sid+" read it, with the rights "+hex(e.mask), removeGrant(path, e.sid), ErrOthersRead)
		}
	}
	return nil
}

func removeGrant(path, sid string) Fix { return Fix{Path: path, Revoke: sid} }

func hex(m uint32) string { return "0x" + strconv.FormatUint(uint64(m), 16) }

// private is whether k is a kind only its owner may read: its group and others may not.
func private(k Kind) bool { return k == Key || k == PrivateFolder }

// macReads is the rights of a macOS access list another account may not have on a name of kind k:
// none but write for most kinds, read for a key, and read and search for a private folder, as its
// mode's 077 bits refuse search too.
func macReads(k Kind) uint32 {
	if k == PrivateFolder {
		return macRead | macSearch
	}
	if k == Key {
		return macRead
	}
	return 0
}

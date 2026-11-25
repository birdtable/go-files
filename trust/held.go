package trust

import (
	"errors"
	"os"
	"slices"
)

// Held judges f, a file or folder already open, as Path judges the last name of a path of the
// kind last for who: its type, owner, mode bits and access lists, and the volume it is on, each
// read on f's own descriptor, never by a name, so a file swapped in at the name after the open is
// not the one judged. It walks no folder above f: whether the name that reached it
// could be changed is the caller's, which walks it with Path before the open. So it does not hold
// a file to root's sticky folder, as Path holds a name right in one, and Who.NoLinks has nothing
// to refuse, as a file held is never a link. Who.Apart is judged by f's file ID: f is refused
// when it is a folder of Who.Apart itself, but a file below one passes Held, and the caller's
// Path, which walks the folders above it, is what refuses that (ErrInside). Who.Overflow,
// Who.Group, Who.FileSystems and Who.Mounts are judged as by Path. On macOS the access list is read by
// f.Name(), held to the descriptor by its device and inode, and a name that names another file,
// or none, at each of eight tries is refused with ErrReplaced. Each refusal is an *Unsafe whose
// Path is f.Name(), with no Fix: a Fix acts by a name, and the name may lead elsewhere than the
// file judged, through a link at it or another file renamed over it, so a fix for the name is
// Path's to give, which reads the name it judges.
func Held(f *os.File, last Kind, who Who) error { return held(f, last, who, local()) }

// held is Held on the host h.
func held(f *os.File, last Kind, who Who, h host) error {
	if f == nil {
		return errors.New("trust.Held: no file is held")
	}
	h = overflow(h, who.Overflow)
	if h.broken != nil {
		return h.broken
	}
	apart, err := apartOf(who.Apart, h)
	if err != nil {
		return err
	}
	if err := mountEntries(who.Mounts, who.FileSystems, h); err != nil {
		return err
	}
	path := f.Name()
	n, err := statWhole(f, h)
	if err != nil {
		return err
	}
	if i := slices.IndexFunc(apart, func(a apartee) bool { return a.id == n.id }); i >= 0 {
		return refuse(path, path+" is "+apart[i].path+", a folder named to be kept apart", Fix{}, ErrInside)
	}
	// A name only its group may write is judged by whether its group bits are a Linux access
	// list's mask, read on the descriptor; a host that cannot tell says it is one.
	if n.dacl == nil && n.perm.Perm()&0o022 == 0o020 {
		n.acl = true
		if h.mask != nil {
			if n.acl, err = h.mask(f); err != nil {
				return err
			}
		}
	}
	return noFix(judge(n, last, place{path: path, last: true}, who, h))
}

// statWhole is h.stat of f, read again while f's name names another file than f, and refused
// with ErrReplaced when it does at every try.
func statWhole(f *os.File, h host) (node, error) {
	for range tries {
		n, err := h.stat(f)
		if !errors.Is(err, errMoved) {
			return n, err
		}
	}
	return node{}, replaced(f.Name())
}

// noFix is err, a refusal of the file held, with no Fix: Held judges a file and not a name, and a
// Fix built from f.Name() would change what the name leads to, which may be a link to the file or
// another file renamed over it, never read here.
func noFix(err error) error {
	if u, ok := errors.AsType[*Unsafe](err); ok && u.Fix != (Fix{}) {
		u.Fix = Fix{}
		u.Observed += "; Held gives no fix, as a fix acts by the name and Held judges the file it holds: Path gives the name's"
	}
	return err
}

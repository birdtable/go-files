package trust

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
)

// ReadOwned reads the whole file at path, opened so that it never waits (a pipe at the name is
// refused at once) and never through a link at the name, and checked on the descriptor opened,
// never on the name, so a file swapped in at path after the open is not the one read. The file
// must be a regular file owner owns, "uid:1000" on Unix and a SID such as "S-1-5-18" on Windows;
// no other account may write it, nor read it when private; and it may hold at most limit bytes. A
// file over the limit, or one that grows past it while it is read, is refused whole, never cut
// short. On macOS, where the list is read by the name, a file replaced at path after the open is
// opened and checked again, and one replaced at each try is refused with ErrReplaced. Each refusal
// is an *Unsafe matching its own error. A file on a mount whose owners are not judged is refused
// with ErrMount, as by Path, and of each of mounts only FileSystems, Mounts and Overflow are read:
// a folder of Mounts is judged as Path judges it, before the file is opened, and refused with
// ErrMountEntry, the names above it walked with the overflow account trusted as root where any of
// mounts sets Who.Overflow and Path would take it, and refused with ErrOverflow, reading nothing,
// where Path would not.
func ReadOwned(path, owner string, private bool, limit int, mounts ...Who) ([]byte, error) {
	h := local()
	on := false
	for _, w := range mounts {
		h.mounts.FileSystems = append(h.mounts.FileSystems, w.FileSystems...)
		h.mounts.Mounts = append(h.mounts.Mounts, w.Mounts...)
		on = on || w.Overflow
	}
	return readOwned(path, owner, private, limit, overflow(h, on), func() {})
}

var ownerForm = regexp.MustCompile(`^(uid:(0|[1-9][0-9]{0,9})|S-1(-[0-9]+)+)$`)

// readOwned is ReadOwned on the host h, calling opened each time the file is open and before it
// is checked, where another process could swap the file at path, so a test of this package does it.
func readOwned(path, owner string, private bool, limit int, h host, opened func()) ([]byte, error) {
	if h.broken != nil {
		return nil, h.broken
	}
	if limit <= 0 {
		return nil, refuse(path, fmt.Sprintf("%s: a limit of %d bytes is no limit, and every read has one", path, limit), Fix{}, nil)
	}
	if !ownerForm.MatchString(owner) {
		return nil, refuse(path, fmt.Sprintf("%s: %q is not an owner, as uid:1000 or a SID", path, owner), Fix{}, nil)
	}
	if err := mountEntries(h.mounts.Mounts, h.mounts.FileSystems, h); err != nil {
		return nil, err
	}
	// A name replaced by rename after the open, where the list is read by the name, is opened
	// again and the new descriptor checked whole, as a second call would.
	for range tries {
		b, err := readOnce(path, owner, private, limit, h, opened)
		if !errors.Is(err, errMoved) {
			return b, err
		}
	}
	return nil, replaced(path)
}

func readOnce(path, owner string, private bool, limit int, h host, opened func()) ([]byte, error) {
	f, err := h.open(path)
	if err != nil {
		return nil, err
	}
	opened()
	b, err := readFrom(f, path, owner, private, limit, h)
	if cerr := f.Close(); cerr != nil && err == nil {
		return nil, cerr
	}
	return b, err
}

func readFrom(f *os.File, path, owner string, private bool, limit int, h host) ([]byte, error) {
	n, err := h.stat(f)
	if err != nil {
		return nil, err
	}
	if err := judgeRead(n, path, owner, private, limit, h); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.NewSectionReader(f, 0, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > limit {
		return nil, refuse(path, fmt.Sprintf("%s grew past %d bytes while it was read", path, limit), Fix{}, ErrTooLong)
	}
	return b, nil
}

// judgeRead is n, a file's facts read on its descriptor, against what ReadOwned asks: a regular
// file of owner, that no other account can write, nor read when private, within limit.
func judgeRead(n node, path, owner string, private bool, limit int, h host) error {
	if n.typ != 0 {
		return refuse(path, path+" is "+typeName(n.typ)+", not a regular file", Fix{}, ErrNotPlain)
	}
	if n.ownersIgnored != "" {
		return ignoresOwners(n, path)
	}
	if why := plainRefused(n, h.mounts, h); why != "" {
		return refuse(path, path+" is on a file system "+why, Fix{}, ErrMount)
	}
	if n.mounter != "" && n.mounter != owner && !slices.Contains(h.root, n.mounter) {
		return mountedBy(n, path)
	}
	if n.owner != owner {
		return refuse(path, path+" is owned by "+n.owner+", not by "+owner, Fix{}, ErrNotOwners)
	}
	if n.dacl != nil {
		if err := othersACL(n, path, owner, private, h); err != nil {
			return err
		}
	} else if err := othersMode(n, path, private); err != nil {
		return err
	}
	if err := judgeMac(n, path, readsIf(private), false, func(e macACE) bool {
		return e.who == owner || trusted(e.who, Who{RootOnly: true}, h)
	}); err != nil {
		return err
	}
	if n.size > int64(limit) {
		return refuse(path, path+" holds "+strconv.FormatInt(n.size, 10)+" bytes, over "+strconv.Itoa(limit), Fix{}, ErrTooLong)
	}
	return nil
}

// othersMode refuses a file whose mode lets its group or others write it, or read it when private.
// The fix of a private file takes reading away with writing, so one change makes it pass.
func othersMode(n node, path string, private bool) error {
	perm := n.perm.Perm()
	fix := Fix{Path: path, Clear: 0o022}
	if private {
		fix.Clear = 0o066
	}
	switch {
	case perm&0o022 != 0:
		return refuse(path, path+" can be written by "+writers(perm), fix, ErrOthersWrite)
	case private && perm&0o044 != 0:
		return refuse(path, path+" can be read by its group or others, and is to be private", Fix{Path: path, Clear: 0o044}, ErrOthersRead)
	}
	return nil
}

// othersACL refuses a file whose access list lets an account besides owner and root's write it,
// or read it when private, by judgeEntries. An AppContainer's entry is not judged, as in judgeACL.
func othersACL(n node, path, owner string, private bool, h host) error {
	return judgeEntries(n.dacl, path, private, false, func(e ace) bool {
		return e.sandbox || e.sid == owner || trusted(e.sid, Who{RootOnly: true}, h)
	})
}

// readsIf is macRead for a private file, and no read right otherwise.
func readsIf(private bool) uint32 {
	if private {
		return macRead
	}
	return 0
}

package trust

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// mountEntries judges each folder of mounts, a Who.Mounts, as an entry the walk may lend a mount's
// pass to, at every call and before the path itself is walked: it is absolute and
// clean; every name above it is root-only, as a walk of its parent with Who.RootOnly judges it, so
// no account but root can put another folder at its name; it is no link; and it is the root of a
// mount, which h.mountRoot reads, so a folder that names only the disk beneath, as an entry whose
// disk is not mounted, lends nothing. Its owner is not judged: root made the mount, and an account
// may own the folder it is mounted on, as /run/user/<uid> is. The entries are judged shallowest
// first, and the walk of each one's parent takes as its Who.Mounts the entries already judged
// above it, so a named /var/tmp under a named nosuid /var passes, and no entry vouches for itself
// or for one above it. The first entry refused is an *Unsafe whose Path is that
// entry, matching ErrMountEntry; one that names nothing is refused so too, never matching
// fs.ErrNotExist, which a caller would read as a name not yet made. A host whose mountRoot is nil,
// as Windows', where Who.Mounts is not read, judges none.
func mountEntries(mounts []string, fileSystems []uint32, h host) error {
	if h.mountRoot == nil {
		return nil
	}
	sep := string(filepath.Separator)
	depth := func(m string) int { return strings.Count(strings.TrimSuffix(m, sep), sep) }
	byDepth := slices.Clone(mounts)
	slices.SortStableFunc(byDepth, func(a, b string) int { return depth(a) - depth(b) })
	judged := make([]string, 0, len(byDepth))
	for _, m := range byDepth {
		var above []string
		for _, e := range judged {
			if e != m && strings.HasPrefix(m, strings.TrimSuffix(e, sep)+sep) {
				above = append(above, e)
			}
		}
		if err := mountEntry(m, fileSystems, above, h); err != nil {
			return err
		}
		judged = append(judged, m)
	}
	return nil
}

// mountEntry judges m, one folder of Who.Mounts, as mountEntries says, with fileSystems the
// caller's Who.FileSystems and above the entries judged above it, for the names above it.
func mountEntry(m string, fileSystems []uint32, above []string, h host) error {
	q := strconv.Quote(m)
	if !filepath.IsAbs(m) || filepath.Clean(m) != m {
		return refuse(m, "the mounts named as trusted hold "+q+", which is not an absolute, clean path", Fix{}, ErrMountEntry)
	}
	gone := func(err error) error {
		return refuse(m, "the mounts named as trusted hold "+q+", which names nothing: "+err.Error(), Fix{}, ErrMountEntry)
	}
	parent := filepath.Dir(m)
	if _, err := walkJudged(parent, Folder, Who{RootOnly: true, FileSystems: fileSystems, Mounts: above}, h, nil); err != nil {
		u, ok := errors.AsType[*Unsafe](err)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return gone(err)
		case !ok:
			return err
		}
		return refuse(m, "the mounts named as trusted hold "+q+", and a name above it is not one only root may change: "+u.Observed, Fix{},
			fmt.Errorf("%w: %w", ErrMountEntry, u))
	}
	n, err := h.look(m)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return gone(err)
	case err != nil:
		return err
	}
	if n.typ == fs.ModeSymlink {
		return refuse(m, "the mounts named as trusted hold "+q+", a link, which any account that can change it points at another disk", Fix{}, ErrMountEntry)
	}
	root, err := h.mountRoot(m, parent)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return gone(err)
	case err != nil:
		return refuse(m, "the mounts named as trusted hold "+q+", whose mount cannot be told: "+err.Error(), Fix{}, fmt.Errorf("%w: %w", ErrMountEntry, err))
	case !root:
		return refuse(m, "the mounts named as trusted hold "+q+", which is no mount's root: it names the disk beneath it, and lends that disk a pass", Fix{}, ErrMountEntry)
	}
	return nil
}

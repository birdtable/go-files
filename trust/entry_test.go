package trust

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// entryHost is the host of dk with mountRoot answering from roots, each entry a mount's root
// where true, none where false, and the error it maps to where it is one; every call is kept in
// asked as entry and parent, joined by a space.
func entryHost(dk *fake, roots map[string]any, asked *[]string) host {
	h := dk.host(meID)
	h.ident = func(p string) (fileID, error) {
		n, ok := dk.nodes[p]
		if !ok {
			return fileID{}, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
		}
		return n.id, nil
	}
	h.mountRoot = func(entry, parent string) (bool, error) {
		*asked = append(*asked, entry+" "+parent)
		switch r := roots[entry].(type) {
		case bool:
			return r, nil
		case error:
			return false, r
		}
		return false, &fs.PathError{Op: "statx", Path: entry, Err: fs.ErrNotExist}
	}
	return h
}

// Each folder of Who.Mounts is judged at every walk, before the path: absolute and clean, only
// names root may change above it, no link, and a mount's root, whoever owns it. One that fails is
// refused with ErrMountEntry, naming it, and the path is not walked; one that cannot be read is an
// error, never a pass. A name on a disk mounted nosuid passes below an entry that holds, as an
// account's /run/user folder, and is refused without it.
//
// What it costs when this fails: a folder an account made, or a link it can point anywhere, or
// one that is no mount's root and so names the disk beneath it, lends a mount's pass to a disk
// another account controls, and a program or key on it is trusted.
func TestEachFolderOfWhoMountsIsJudgedBeforeThePathAndRefusedWithErrMountEntry(t *testing.T) {
	t.Parallel()
	const ext4 = 0xEF53
	mnt, run, home := at("mnt"), at("run"), at("home")
	stick, user, mine := at("run", "user", "501"), at("run", "user"), at("home", "me")
	f := filepath.Join(stick, "f")
	nosuid := func(n node) node { n.fsType, n.nosuid, n.id.vol = ext4, true, 7; return n }
	nodes := map[string]node{
		mnt: dir(rootID, 0o755), run: dir(rootID, 0o755), user: dir(rootID, 0o755), home: dir(rootID, 0o755),
		stick: nosuid(dir(meID, 0o700)), f: nosuid(file(meID, 0o600)),
		mine: dir(meID, 0o755), filepath.Join(mine, "stick"): nosuid(dir(meID, 0o700)),
		filepath.Join(mnt, "l"): link(rootID), filepath.Join(mnt, "plain"): dir(rootID, 0o755), filepath.Join(mnt, "odd"): dir(rootID, 0o755),
	}
	roots := map[string]any{
		stick: true, filepath.Join(mine, "stick"): true, filepath.Join(mnt, "l"): true,
		filepath.Join(mnt, "plain"): false, filepath.Join(mnt, "odd"): errors.New("statx answers no mount"),
	}
	words := "is on a file system a disk backs, mounted nosuid"
	cases := []struct {
		name   string
		mounts []string
		r      refusal
	}{
		{"an account's mount under names only root may change, as /run/user/501", []string{stick}, refusal{}},
		{"none, on a disk mounted nosuid", nil, refusal{ErrMount, Fix{}, words}},
		{"one not absolute", []string{"stick"}, refusal{ErrMountEntry, Fix{}, `the mounts named as trusted hold "stick", which is not an absolute, clean path`}},
		{"one not clean", []string{stick + string(filepath.Separator) + ".." + string(filepath.Separator) + "501"}, refusal{ErrMountEntry, Fix{}, "which is not an absolute, clean path"}},
		{"one under a folder this account may change", []string{filepath.Join(mine, "stick")}, refusal{ErrMountEntry, Fix{}, "a name above it is not one only root may change"}},
		{"a link, though root's", []string{filepath.Join(mnt, "l")}, refusal{ErrMountEntry, Fix{}, "a link"}},
		{"a folder of root's that is no mount's root", []string{filepath.Join(mnt, "plain")}, refusal{ErrMountEntry, Fix{}, "which is no mount's root"}},
		{"one whose mount cannot be told", []string{filepath.Join(mnt, "odd")}, refusal{ErrMountEntry, Fix{}, "whose mount cannot be told"}},
		{"a good one, then a bad one", []string{stick, filepath.Join(mnt, "plain")}, refusal{ErrMountEntry, Fix{}, "which is no mount's root"}},
	}
	for _, c := range cases {
		var asked []string
		_, err := walk(f, File, Who{Mounts: c.mounts}, entryHost(disk(nodes), roots, &asked))
		wantRefused(t, c.name, err, c.r)
		if u, ok := errors.AsType[*Unsafe](err); ok && errors.Is(err, ErrMountEntry) && u.Path != c.mounts[len(c.mounts)-1] {
			t.Errorf("%s: Unsafe.Path is %q, and want the entry %q", c.name, u.Path, c.mounts[len(c.mounts)-1])
		}
		if errors.Is(c.r.want, ErrMountEntry) && !errors.Is(err, ErrMount) {
			t.Errorf("%s: %v does not match ErrMount too", c.name, err)
		}
	}
	if got := ErrMountEntry.Error(); !strings.HasPrefix(got, "names as trusted a folder that is not a mount's root at a name only root can move: ") || !errors.Is(ErrMountEntry, ErrMount) {
		t.Errorf("ErrMountEntry says %q, and want it to name the folder as trusted, no field of Who, and match ErrMount", got)
	}

	// Nested entries, as a folder bound over itself on a disk mounted nosuid at /srv: the walk above
	// each takes the entries already judged above it, shallowest first in whatever order they are
	// given, and none vouches for itself, which a bind on its parent's device would.
	srv, inner := at("srv"), at("srv", "x")
	onSrv := func(n node) node { n.fsType, n.nosuid, n.id.vol = ext4, true, 8; return n }
	nodes[srv], nodes[inner], nodes[filepath.Join(inner, "f")] = onSrv(dir(rootID, 0o755)), onSrv(dir(rootID, 0o755)), onSrv(file(meID, 0o600))
	roots[srv], roots[inner] = true, true
	for _, c := range []struct {
		name   string
		mounts []string
		r      refusal
	}{
		{"both named, the shallower first", []string{srv, inner}, refusal{}},
		{"both named, the deeper first", []string{inner, srv}, refusal{}},
		{"the deeper alone, its nosuid ancestor left out", []string{inner}, refusal{ErrMountEntry, Fix{}, "a name above it is not one only root may change"}},
	} {
		var asked []string
		_, err := walk(filepath.Join(inner, "f"), File, Who{Mounts: c.mounts}, entryHost(disk(nodes), roots, &asked))
		wantRefused(t, "nested entries, "+c.name, err, c.r)
		if u, ok := errors.AsType[*Unsafe](err); ok && u.Path != inner {
			t.Errorf("nested entries, %s: Unsafe.Path is %q, and want the entry %q", c.name, u.Path, inner)
		}
	}

	var asked []string
	_, err := walk(f, File, Who{Mounts: []string{stick}}, entryHost(disk(nodes), roots, &asked))
	if err != nil || len(asked) != 1 || asked[0] != stick+" "+user {
		t.Errorf("a walk with %s asked mountRoot %q (%v), and want it asked once, with the folder above", stick, asked, err)
	}
	// An entry that names nothing is refused, never fs.ErrNotExist, which appdirs' tempBase reads
	// as a name not yet made: one gone, one under a folder gone, one gone between
	// its look and mountRoot's.
	nodes[filepath.Join(mnt, "racing")] = dir(rootID, 0o755)
	for _, gone := range []string{filepath.Join(mnt, "gone"), at("nowhere", "stick"), filepath.Join(mnt, "racing")} {
		_, err := walk(f, File, Who{Mounts: []string{gone}}, entryHost(disk(nodes), roots, &asked))
		wantRefused(t, gone+", which names nothing", err, refusal{ErrMountEntry, Fix{}, "which names nothing"})
		if errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s, which names nothing: %v matches fs.ErrNotExist, which a caller reads as a name not yet made", gone, err)
		}
	}
	shut := filepath.Join(mnt, "shut")
	h := entryHost(disk(nodes), roots, &asked)
	look := h.look
	h.look = func(p string) (node, error) {
		if p == shut {
			return node{}, &fs.PathError{Op: "lstat", Path: p, Err: fs.ErrPermission}
		}
		return look(p)
	}
	if _, err := walk(f, File, Who{Mounts: []string{shut}}, h); !errors.Is(err, fs.ErrPermission) || errors.Is(err, ErrMountEntry) {
		t.Errorf("an entry that cannot be read: %v, and want the error that reads it, as it is, never a pass", err)
	}
	if _, err := walk(at("nowhere"), File, Who{Mounts: []string{"stick"}}, entryHost(disk(nodes), roots, &asked)); !errors.Is(err, ErrMountEntry) {
		t.Errorf("a bad entry and a path that names nothing: %v, and want ErrMountEntry, as the entry is judged first", err)
	}
	h = entryHost(disk(nodes), roots, &asked)
	h.mountRoot = nil
	if _, err := walk(f, File, Who{Mounts: []string{"stick"}}, h); err == nil || errors.Is(err, ErrMountEntry) {
		t.Errorf("a host that tells no mount's root, as Windows: %v, and want no entry judged, and the name refused by its mount", err)
	}
}

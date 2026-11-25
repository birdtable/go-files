package trust

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The tests of this file judge facts a fake host hands the walk, so every rule, the Windows ones
// among them, is tested on every system, and on facts no account but root could make on a disk.

// The owners of the fake host: root's, this account's and another's, as Unix names them and as
// Windows does.
const (
	rootID  = "uid:0"
	meID    = "uid:501"
	otherID = "uid:7"
	userSID = "S-1-5-21-1-2-3-1001"
	evilSID = "S-1-5-21-1-2-3-1002"
	authed  = "S-1-5-11"
)

// top is the top of a path on this system, "/" or a drive.
func top() string {
	if filepath.Separator == '\\' {
		return `C:\`
	}
	return "/"
}

// at is the path of names under top.
func at(names ...string) string { return filepath.Join(append([]string{top()}, names...)...) }

// fake is a disk of facts: each name's node, and each link's target.
type fake struct {
	nodes map[string]node
	links map[string]string
	// read is every link whose target was read.
	read []string
}

// host is the host that answers from f, trusting rootID and administrators' SIDs, with me as me.
func (f *fake) host(me string) host {
	return host{
		look: func(p string) (node, error) {
			n, ok := f.nodes[p]
			if !ok {
				return node{}, &fs.PathError{Op: "lstat", Path: p, Err: fs.ErrNotExist}
			}
			return n, nil
		},
		readlink: func(p string) (string, error) {
			f.read = append(f.read, p)
			t, ok := f.links[p]
			if !ok {
				return "", &fs.PathError{Op: "readlink", Path: p, Err: fs.ErrNotExist}
			}
			return t, nil
		},
		root: []string{rootID, "S-1-5-32-544", "S-1-5-18"},
		me:   me,
	}
}

// dir, file and link are a Unix node of owner and permission bits.
func dir(owner string, perm fs.FileMode) node  { return node{typ: fs.ModeDir, owner: owner, perm: perm} }
func file(owner string, perm fs.FileMode) node { return node{owner: owner, perm: perm} }
func link(owner string) node                   { return node{typ: fs.ModeSymlink, owner: owner, perm: 0o777} }

// disk is a fake whose top is root's and open to none, holding nodes besides.
func disk(nodes map[string]node) *fake {
	all := map[string]node{top(): dir(rootID, 0o755)}
	maps.Copy(all, nodes)
	return &fake{nodes: all, links: map[string]string{}}
}

// refusal is what a check is to answer: no error, or an *Unsafe matching want, with the fix fix
// and words that hold says.
type refusal struct {
	want error
	fix  Fix
	says string
}

// wantRefused fails t unless err is r: nil when r is the zero refusal, else an *Unsafe that
// matches r.want (none when r.want is nil), whose Fix is r.fix, whose Observed holds r.says, and
// whose Path is a name its Observed names, as written or quoted (a quoted Windows path doubles each
// backslash): "" only for a setting of Who, or for the path "" asked for.
func wantRefused(t *testing.T, name string, err error, r refusal) {
	t.Helper()
	if r == (refusal{}) {
		if err != nil {
			t.Errorf("%s: %v, and want a pass", name, err)
		}
		return
	}
	u, ok := errors.AsType[*Unsafe](err)
	switch {
	case !ok:
		t.Errorf("%s: %v, and want an *Unsafe", name, err)
	case r.want == nil && u.Unwrap() != nil, r.want != nil && !errors.Is(err, r.want):
		t.Errorf("%s: matches %v, and want %v", name, u.Unwrap(), r.want)
	case u.Fix != r.fix:
		t.Errorf("%s: the fix is %+v, and want %+v", name, u.Fix, r.fix)
	case !strings.Contains(u.Observed, r.says):
		t.Errorf("%s: %q does not say %q", name, u.Observed, r.says)
	case !strings.Contains(u.Observed, u.Path) && !strings.Contains(u.Observed, strconv.Quote(u.Path)),
		u.Path == "" && !strings.HasPrefix(u.Observed, "the ") && !strings.HasPrefix(u.Observed, `""`):
		t.Errorf("%s: Unsafe.Path is %q, which %q does not name", name, u.Path, u.Observed)
	}
}

// What it costs when this fails: a file, key, program or folder another account owns or can
// change passes, and a program runs or reads what that account wrote; or a refusal names no fix
// a person can run.
func TestEachRuleOfTheModeBitsRefusesItsNameWithItsFix(t *testing.T) {
	t.Parallel()
	p := at("p")
	ours := func(gid uint32) bool { return gid == 80 }
	grouped := func(n node) node { n.gid = 80; return n }
	masked := func(n node) node { n.gid, n.acl = 80, true; return n }
	cases := []struct {
		name string
		n    node
		k    Kind
		who  Who
		r    refusal
	}{
		{"a file of this account's", file(meID, 0o644), File, Who{}, refusal{}},
		{"a file of root's", file(rootID, 0o644), File, Who{}, refusal{}},
		{"a file of another's", file(otherID, 0o644), File, Who{}, refusal{ErrNotOwners, Fix{Path: p, Owner: "uid:0"}, "is owned by " + otherID}},
		{"root alone: a file of this account's", file(meID, 0o644), File, Who{RootOnly: true}, refusal{ErrNotOwners, Fix{Path: p, Owner: "uid:0"}, "not trusted"}},
		{"a file its group can write", file(meID, 0o664), File, Who{}, refusal{ErrOthersWrite, Fix{Path: p, Clear: 0o022}, "written by its group"}},
		{"a file others can write", file(meID, 0o646), File, Who{}, refusal{ErrOthersWrite, Fix{Path: p, Clear: 0o022}, "written by others"}},
		{"a file its group and others can write", file(rootID, 0o666), File, Who{}, refusal{ErrOthersWrite, Fix{Path: p, Clear: 0o022}, "its group and others"}},
		{"a file its group can write, of a group asked", grouped(file(meID, 0o664)), File, Who{Group: ours}, refusal{}},
		{"a file its group can write, of a group not asked", file(meID, 0o664), File, Who{Group: ours}, refusal{ErrOthersWrite, Fix{Path: p, Clear: 0o022}, "its group"}},
		{"a file others can write, of a group asked", grouped(file(meID, 0o666)), File, Who{Group: ours}, refusal{ErrOthersWrite, Fix{Path: p, Clear: 0o022}, "its group and others"}},
		{"a file whose group bits are an access list's mask", masked(file(meID, 0o664)), File, Who{Group: ours}, refusal{ErrOthersWrite, Fix{Path: p, Clear: 0o020}, "mask of an access list"}},
		{"root alone: a file of a group asked", grouped(file(rootID, 0o664)), File, Who{RootOnly: true, Group: ours}, refusal{ErrOthersWrite, Fix{Path: p, Clear: 0o022}, "its group"}},
		{"a folder its group can write, of a group asked", grouped(dir(meID, 0o775)), Folder, Who{Group: ours}, refusal{}},
		{"a folder its group can write", grouped(dir(meID, 0o775)), Folder, Who{}, refusal{ErrOthersWrite, Fix{Path: p, Clear: 0o022}, "its group"}},
		{"a key of this account's alone", file(meID, 0o600), Key, Who{}, refusal{}},
		{"a key its group can read", file(meID, 0o640), Key, Who{}, refusal{ErrOthersRead, Fix{Path: p, Clear: 0o077}, "read or written"}},
		{"a key others can read", file(meID, 0o604), Key, Who{}, refusal{ErrOthersRead, Fix{Path: p, Clear: 0o077}, "read or written"}},
		{"a key its group may execute", file(meID, 0o610), Key, Who{}, refusal{ErrOthersRead, Fix{Path: p, Clear: 0o077}, "read or written"}},
		{"a private folder of this account's alone", dir(meID, 0o700), PrivateFolder, Who{}, refusal{}},
		{"a private folder its group can read", dir(meID, 0o750), PrivateFolder, Who{}, refusal{ErrOthersRead, Fix{Path: p, Clear: 0o077}, "has the mode 0750"}},
		{"a private folder others can read", dir(meID, 0o705), PrivateFolder, Who{}, refusal{ErrOthersRead, Fix{Path: p, Clear: 0o077}, "has the mode 0705"}},
		{"a private folder its group can write, of a group asked", grouped(dir(meID, 0o770)), PrivateFolder, Who{Group: ours}, refusal{ErrOthersRead, Fix{Path: p, Clear: 0o077}, "has the mode 0770"}},
		{"a file where a private folder is asked", file(meID, 0o600), PrivateFolder, Who{}, refusal{ErrNotPlain, Fix{}, "not a folder"}},
		{"a program", file(rootID, 0o755), Program, Who{}, refusal{}},
		{"a program its owner cannot execute", file(rootID, 0o655), Program, Who{}, refusal{nil, Fix{Path: p, Set: 0o100}, "cannot be executed"}},
		{"a socket", node{typ: fs.ModeSocket, owner: meID, perm: 0o755}, Socket, Who{}, refusal{}},
		{"a socket others can write", node{typ: fs.ModeSocket, owner: meID, perm: 0o777}, Socket, Who{}, refusal{ErrOthersWrite, Fix{Path: p, Clear: 0o022}, "its group and others"}},
		{"a folder where a file is asked", dir(meID, 0o755), File, Who{}, refusal{ErrNotPlain, Fix{}, "is a folder, not a regular file"}},
		{"a file where a folder is asked", file(meID, 0o755), Folder, Who{}, refusal{ErrNotPlain, Fix{}, "is a regular file, not a folder"}},
		{"a file where a socket is asked", file(meID, 0o755), Socket, Who{}, refusal{ErrNotPlain, Fix{}, "not a socket"}},
		{"a pipe where a file is asked", node{typ: fs.ModeNamedPipe, owner: meID, perm: 0o644}, File, Who{}, refusal{ErrNotPlain, Fix{}, "of type"}},
		{"a device where a key is asked", node{typ: fs.ModeDevice | fs.ModeCharDevice, owner: rootID, perm: 0o600}, Key, Who{}, refusal{ErrNotPlain, Fix{}, "of type"}},
		{"a sticky folder of root's as the last name", dir(rootID, fs.ModeSticky|0o777), Folder, Who{}, refusal{ErrOthersWrite, Fix{Path: p, Clear: 0o022}, "its group and others"}},
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

// What it costs when this fails: a folder on the way that another account can add to, rename in
// or remove from passes, and that account swaps the file below it for its own.
func TestAFolderOthersCanWritePassesOnlyAsRootsStickyFolderAboveTheLastName(t *testing.T) {
	t.Parallel()
	o, f := at("o"), at("o", "f")
	cases := []struct {
		name string
		o    node
		who  Who
		r    refusal
	}{
		{"root's, sticky", dir(rootID, fs.ModeSticky|0o777), Who{}, refusal{}},
		{"root's, sticky, its group alone can write", dir(rootID, fs.ModeSticky|0o775), Who{}, refusal{}},
		{"root's, with no sticky bit", dir(rootID, 0o777), Who{}, refusal{ErrOthersWrite, Fix{Path: o, Clear: 0o022}, "its group and others"}},
		{"this account's, sticky", dir(meID, fs.ModeSticky|0o777), Who{}, refusal{ErrOthersWrite, Fix{Path: o, Clear: 0o022}, "its group and others"}},
		{"root alone: root's, sticky", dir(rootID, fs.ModeSticky|0o777), Who{RootOnly: true}, refusal{ErrOthersWrite, Fix{Path: o, Clear: 0o022}, "its group and others"}},
	}
	for _, c := range cases {
		d := disk(map[string]node{o: c.o, f: dir(meID, 0o700)})
		_, err := walk(f, Folder, c.who, d.host(meID))
		wantRefused(t, c.name, err, c.r)
	}
}

// What it costs when this fails: another account adds a hard link in /tmp to a file of root's or
// of this account's, or a link of root's, and the walk passes the name it chose as that file.
func TestANameRightInRootsStickyFolderIsHeldFurther(t *testing.T) {
	t.Parallel()
	tmp := at("tmp")
	sticky := dir(rootID, fs.ModeSticky|0o777)
	cases := []struct {
		name  string
		names []string
		n     node
		k     Kind
		r     refusal
	}{
		{"a file of this account's", []string{"f"}, file(meID, 0o644), File, refusal{ErrNotOwners, Fix{}, "is right in " + tmp}},
		{"a file of root's", []string{"f"}, file(rootID, 0o644), File, refusal{ErrNotOwners, Fix{}, "is right in " + tmp}},
		{"a key of this account's", []string{"f"}, file(meID, 0o600), Key, refusal{ErrNotOwners, Fix{}, "is right in " + tmp}},
		{"a program of root's", []string{"f"}, file(rootID, 0o755), Program, refusal{ErrNotOwners, Fix{}, "is right in " + tmp}},
		{"a socket of this account's", []string{"f"}, node{typ: fs.ModeSocket, owner: meID, perm: 0o755}, Socket, refusal{ErrNotOwners, Fix{}, "is right in " + tmp}},
		{"a folder of root's", []string{"f"}, dir(rootID, 0o755), Folder, refusal{ErrNotOwners, Fix{}, "is root's, right in " + tmp}},
		{"a folder of another's", []string{"f"}, dir(otherID, 0o755), Folder, refusal{ErrNotOwners, Fix{}, "is owned by"}},
		{"a folder of this account's", []string{"f"}, dir(meID, 0o700), Folder, refusal{}},
		{"a folder of this account's on another device, as a FUSE mount", []string{"f"}, onDevice(dir(meID, 0o700), 2), Folder, refusal{ErrNotOwners, Fix{}, "on another device than it"}},
	}
	for _, c := range cases {
		p := at(append([]string{"tmp"}, c.names...)...)
		d := disk(map[string]node{tmp: sticky, p: c.n})
		_, err := walk(p, c.k, Who{}, d.host(meID))
		wantRefused(t, c.name, err, c.r)
	}
	d := disk(map[string]node{tmp: sticky, at("tmp", "m"): dir(meID, 0o700), at("tmp", "m", "f"): file(meID, 0o600)})
	_, err := walk(at("tmp", "m", "f"), Key, Who{}, d.host(meID))
	wantRefused(t, "a key in a folder of this account's in /tmp", err, refusal{})
}

// What it costs when this fails: a program run as root, as in a container, has no folder of its
// own in /tmp; or, the other way, a folder another account serves through a mount there, or a
// folder of root's there, passes as this account's own.
func TestRootsOwnFolderRightInRootsStickyFolderPassesForRootAloneAndOnItsDevice(t *testing.T) {
	t.Parallel()
	tmp, r := at("tmp"), at("tmp", "r")
	sticky := dir(rootID, fs.ModeSticky|0o777)
	sticky.id.vol = 1
	mine := dir(rootID, 0o700)
	mine.id.vol = 1
	mounted := mine
	mounted.id.vol = 2
	cases := []struct {
		name string
		n    node
		me   string
		r    refusal
	}{
		{"root's own folder, as root", mine, rootID, refusal{}},
		{"root's own folder, as root, with others' write", func() node { n := mine; n.perm = 0o777; return n }(), rootID, refusal{ErrOthersWrite, Fix{Path: r, Clear: 0o022}, "its group and others"}},
		{"root's own folder on another device, as root", mounted, rootID, refusal{ErrNotOwners, Fix{}, "on another device than it"}},
		{"root's folder, as another account", mine, meID, refusal{ErrNotOwners, Fix{}, "is root's, right in " + tmp}},
		{"root's folder on another device, as another account", mounted, meID, refusal{ErrNotOwners, Fix{}, "on another device than it"}},
		{"another's folder, as root", func() node { n := mine; n.owner = otherID; return n }(), rootID, refusal{ErrNotOwners, Fix{Path: r, Owner: "uid:0"}, "is owned by"}},
		{"another's folder on another device, as another account", func() node { n := mounted; n.owner = otherID; return n }(), meID, refusal{ErrNotOwners, Fix{}, "on another device than it"}},
	}
	for _, c := range cases {
		d := disk(map[string]node{tmp: sticky, r: c.n})
		_, err := walk(r, Folder, Who{}, d.host(c.me))
		wantRefused(t, c.name, err, c.r)
	}
}

// What it costs when this fails: a link in /tmp, of root's or of this account's, which another
// account can add a hard link to, passes, and the walk follows where that account's name points.
func TestALinkRightInRootsStickyFolderIsRefusedWhoeverOwnsIt(t *testing.T) {
	t.Parallel()
	tmp, l, f := at("tmp"), at("tmp", "l"), at("etc", "f")
	for _, owner := range []string{rootID, meID} {
		d := disk(map[string]node{tmp: dir(rootID, fs.ModeSticky|0o777), l: link(owner), at("etc"): dir(rootID, 0o755), f: file(rootID, 0o644)})
		d.links[l] = f
		_, err := walk(l, File, Who{}, d.host(meID))
		wantRefused(t, owner+"'s link in /tmp", err, refusal{ErrNotOwners, Fix{}, "is right in " + tmp})
	}
}

// What it costs when this fails: a link another account owns is read, or a link is followed where
// none may be, and the walk answers a path another account chose.
func TestALinkIsJudgedBeforeItIsReadAndRefusedWhereNoneIsFollowed(t *testing.T) {
	t.Parallel()
	a, l, f := at("a"), at("a", "l"), at("a", "f")
	base := map[string]node{a: dir(rootID, 0o755), l: link(meID), f: file(meID, 0o644), at("a", "f", "x"): file(meID, 0o644)}
	cases := []struct {
		name string
		path string
		ln   node
		who  Who
		r    refusal
	}{
		{"a link at the end, of this account's", l, link(meID), Who{}, refusal{}},
		{"a link at the end, with NoLinks", l, link(meID), Who{NoLinks: true}, refusal{ErrNotPlain, Fix{}, "no link is followed"}},
		{"a link at the end, root alone", l, link(rootID), Who{RootOnly: true}, refusal{ErrNotPlain, Fix{}, "no link is followed"}},
		{"a link in the middle, with NoLinks", filepath.Join(l, "x"), link(meID), Who{NoLinks: true}, refusal{ErrNotPlain, Fix{}, "no link is followed"}},
		{"a link of another's", l, link(otherID), Who{}, refusal{ErrNotOwners, Fix{Path: l, Owner: "uid:0"}, "is owned by " + otherID}},
	}
	for _, c := range cases {
		nodes := maps.Clone(base)
		nodes[l] = c.ln
		d := disk(nodes)
		d.links[l] = "f"
		_, err := walk(c.path, File, c.who, d.host(meID))
		wantRefused(t, c.name, err, c.r)
		if err != nil && len(d.read) != 0 {
			t.Errorf("%s: read the link %v before it passed", c.name, d.read)
		}
	}
}

// What it costs when this fails: the walk answers a path with a link left in it, which another
// account can repoint once the walk is done, or loops on links without end.
func TestALinkIsFollowedFromTheTopAndAPathWithNoneIsAnswered(t *testing.T) {
	t.Parallel()
	nodes := map[string]node{
		at("a"): dir(rootID, 0o755), at("a", "rel"): link(meID), at("a", "abs"): link(meID),
		at("b"): dir(rootID, 0o755), at("b", "c"): dir(meID, 0o755), at("b", "c", "f"): file(meID, 0o644),
	}
	d := disk(nodes)
	d.links[at("a", "rel")] = filepath.Join("..", "b", "c")
	d.links[at("a", "abs")] = at("b", "c")
	for _, p := range []string{at("a", "rel", "f"), at("a", "abs", "f")} {
		got, err := walk(p, File, Who{}, d.host(meID))
		if err != nil || got != at("b", "c", "f") {
			t.Errorf("%s: %q, %v, and want %q", p, got, err, at("b", "c", "f"))
		}
	}
	// A link to itself is followed forty times and refused.
	nodes[at("a", "loop")] = link(meID)
	d = disk(nodes)
	d.links[at("a", "loop")] = "loop"
	_, err := walk(at("a", "loop"), File, Who{}, d.host(meID))
	wantRefused(t, "a loop", err, refusal{nil, Fix{}, "passes more than 40 links"})
	if len(d.read) != maxLinks+1 {
		t.Errorf("read the loop %d times, and want %d", len(d.read), maxLinks+1)
	}
}

// What it costs when this fails: a relative or unclean path is walked from wherever the program
// stands, or a name that cannot be read is passed.
func TestWhatTheWalkCannotReadIsAnErrorNeverAPass(t *testing.T) {
	t.Parallel()
	d := disk(nil)
	for _, p := range []string{"p", filepath.Join("a", "b"), at("a") + string(filepath.Separator) + ".." + string(filepath.Separator) + "b", ""} {
		_, err := walk(p, File, Who{}, d.host(meID))
		wantRefused(t, "the path "+p, err, refusal{nil, Fix{}, "is not an absolute, clean path"})
	}
	if _, err := walk(at("gone"), File, Who{}, d.host(meID)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a name that is not there: %v, and want fs.ErrNotExist", err)
	}
	// Rule 6 on Windows: a descriptor that cannot be read is the host's error, and the walk ends.
	denied := errors.New("the descriptor cannot be read")
	h := d.host(meID)
	h.look = func(string) (node, error) { return node{}, denied }
	if _, err := walk(at("p"), File, Who{}, h); !errors.Is(err, denied) {
		t.Errorf("a name whose facts cannot be read: %v, and want its error", err)
	}
}

// What it costs when this fails: a host that could not write a SID, or read this account's own,
// answers "" for it, and a name whose owner is "" passes as this account's; or the walk and
// ReadOwned judge with a host that knows no account.
func TestAnOwnerOrAnAccountOfNoNameIsNeverTrusted(t *testing.T) {
	t.Parallel()
	nobody := disk(map[string]node{at("p"): dir("", 0o755), at("l"): {typ: fs.ModeSymlink, owner: ""}, at("w"): winDir("")})
	nobody.links[at("l")] = at("p")
	for _, p := range []string{at("p"), at("l"), at("w")} {
		_, err := walk(p, Folder, Who{}, nobody.host(""))
		wantRefused(t, "owner \"\", me \"\": "+p, err, refusal{ErrNotOwners, chown(nobody.nodes[p], p), "is owned by , which is not trusted"})
	}
	h := nobody.host(meID)
	h.root = append(h.root, "")
	_, err := walk(at("p"), Folder, Who{}, h)
	wantRefused(t, "owner \"\" with \"\" among root's", err, refusal{ErrNotOwners, Fix{Path: at("p"), Owner: "uid:0"}, "not trusted"})
	broken := errors.New("this account's SID cannot be read")
	h = disk(map[string]node{at("p"): dir(meID, 0o755)}).host(meID)
	h.broken = broken
	h.open = func(p string) (*os.File, error) { return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist} }
	if _, err := walk(at("p"), Folder, Who{}, h); !errors.Is(err, broken) {
		t.Errorf("the walk on a broken host: %v, and want why it is broken", err)
	}
	if _, err := readOwned(at("p"), meID, false, 1, h, func() {}); !errors.Is(err, broken) {
		t.Errorf("ReadOwned on a broken host: %v, and want why it is broken", err)
	}
}

// Account is the owner the walk trusts as this account, on a Unix host and a Windows one, and on a
// host that cannot read this account's SID it is "" and the walk's own refusal.
//
// What it costs when this fails: a program that reads its own file through ReadOwned as Account
// names an owner the walk does not take for this account, and is refused its own file, or is
// handed an owner on a host the walk refuses.
func TestAccountIsTheOwnerTheWalkTrustsAsThisAccount(t *testing.T) {
	t.Parallel()
	broken := errors.New("this account's SID cannot be read")
	_, win := winDisk(map[string]node{at("p"): winDir(userSID)})
	cases := []struct {
		what   string
		h      host
		broken error
	}{
		{"a Unix host", disk(map[string]node{at("p"): dir(meID, 0o755)}).host(meID), nil},
		{"a Windows host", win, nil},
		{"a host that cannot read this account's SID", disk(map[string]node{at("p"): dir(meID, 0o755)}).host(""), broken},
	}
	for _, c := range cases {
		c.h.broken = c.broken
		got, err := account(c.h)
		_, walked := walk(at("p"), Folder, Who{}, c.h)
		_, rootOnly := walk(at("p"), Folder, Who{RootOnly: true}, c.h)
		switch {
		case c.broken != nil:
			if got != "" || !errors.Is(err, c.broken) || !errors.Is(walked, err) {
				t.Errorf("%s: %q, %v, and the walk %v, and want \"\" and the walk's refusal", c.what, got, err, walked)
			}
		case err != nil || got != c.h.me || !ownerForm.MatchString(got):
			t.Errorf("%s: %q, %v, and want %q in the form ReadOwned takes", c.what, got, err, c.h.me)
		case walked != nil || !errors.Is(rootOnly, ErrNotOwners):
			t.Errorf("%s: a folder %s owns: %v, and root alone trusted %v, and want a pass and ErrNotOwners", c.what, got, walked, rootOnly)
		}
	}
}

// No string in trust's source names a program that changes a name's mode, owner or access list,
// but in commands.go, where each is one word of an argv: a Fix is data, and Fix.Commands gives
// it as argv, but nothing in trust quotes or joins a command line.
// sudo is named nowhere: Step.Admin says a step needs it.
//
// What it costs when this fails: a caller pastes a line trust built into a shell, and a path that
// holds a quote, a space or a trailing backslash runs as more than the one name it was.
func TestNothingInTrustQuotesOrJoinsACommandLine(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0) //nolint:staticcheck // ParseDir reads one folder's files, which is all this needs
	if err != nil {
		t.Fatal(err)
	}
	read, argv := 0, 0
	for _, p := range pkgs {
		for name, f := range p.Files {
			words := filepath.Base(name) == "commands.go"
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				read++
				v := strings.ToLower(lit.Value)
				switch {
				case strings.Contains(v, "sudo"):
					t.Errorf("%s: %s names sudo", fset.Position(lit.Pos()), lit.Value)
				case words && lit.Value[0] == '"' && !strings.ContainsAny(v[1:len(v)-1], " \t\"'`&|;<>$%^()"):
					argv++
				default:
					for _, word := range []string{"chmod", "chown", "icacls", "takeown", "setfacl", "cacls"} {
						if strings.Contains(v, word) {
							t.Errorf("%s: %s names %s, and is not one word of an argv in commands.go", fset.Position(lit.Pos()), lit.Value, word)
						}
					}
				}
				return true
			})
		}
	}
	if read < 20 || argv < 10 {
		t.Fatalf("read %d strings, %d of them argv words", read, argv)
	}
}

// the is n with the file ID of idx, on volume 1.
func the(n node, idx uint64) node { n.id = fileID{vol: 1, idx: idx}; return n }

// apartHost is d's host, whose ident answers the ID of each name of ids and counts its reads.
func apartHost(d *fake, ids map[string]uint64, reads *int) host {
	h := d.host(meID)
	h.ident = func(p string) (fileID, error) {
		*reads++
		idx, ok := ids[p]
		if !ok {
			return fileID{}, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
		}
		return fileID{vol: 1, idx: idx}, nil
	}
	return h
}

// What it costs when this fails: a path inside the work tree, where a head can commit a file the
// tools read, passes because it was spelled through a link, in another case, or as the folder
// itself; or a path beside the tree is refused, and no temp folder can be made.
func TestAPathThatIsOrLiesBelowAnApartFolderIsRefusedByItsFileID(t *testing.T) {
	t.Parallel()
	nodes := map[string]node{
		at("w"): the(dir(meID, 0o755), 2), at("w", "s"): the(dir(meID, 0o755), 3), at("w", "s", "f"): the(file(meID, 0o644), 4),
		// W is the tree in another case, as a volume that ignores case reads it: the same file.
		at("W"): the(dir(meID, 0o755), 2), at("W", "s"): the(dir(meID, 0o755), 3),
		at("l"): the(link(meID), 5), at("o"): the(dir(meID, 0o755), 6), at("o", "f"): the(file(meID, 0o644), 7),
		// v is a folder on another volume whose file index is the tree's: not the same file.
		at("v"): {typ: fs.ModeDir, owner: meID, perm: 0o755, id: fileID{vol: 2, idx: 2}},
	}
	d := disk(nodes)
	d.links[at("l")] = at("w", "s")
	ids := map[string]uint64{at("w"): 2, at("lw"): 2, at("o"): 6}
	for _, c := range []struct {
		path, apart string
		k           Kind
		says        string
	}{
		{at("w"), at("w"), Folder, at("w") + " is " + at("w") + ", a folder named to be kept apart"},
		{at("w", "s", "f"), at("w"), File, at("w") + " is " + at("w") + ", a folder named to be kept apart"},
		{at("W", "s"), at("w"), Folder, at("W") + " is " + at("w") + ", a folder named to be kept apart"},
		{filepath.Join(at("l"), "f"), at("w"), File, at("w") + " is " + at("w") + ", a folder named to be kept apart"},
		// The apart folder named through a link of its own is read as what it names.
		{at("w", "s", "f"), at("lw"), File, at("w") + " is " + at("lw") + ", a folder named to be kept apart"},
	} {
		reads := 0
		_, err := walk(c.path, c.k, Who{Apart: []string{at("o"), c.apart}}, apartHost(d, ids, &reads))
		wantRefused(t, c.path+" apart from "+c.apart, err, refusal{ErrInside, Fix{}, c.says})
		if reads != 2 {
			t.Errorf("%s: the apart folders were read %d times, and want once each, 2", c.path, reads)
		}
	}
	for _, p := range []string{at("W", "s"), at("l")} {
		if _, err := walk(p, Folder, Who{Apart: []string{at("o")}}, apartHost(d, ids, new(int))); err != nil {
			t.Errorf("%s, beside the apart folder: %v, and want a pass", p, err)
		}
	}
	for p, k := range map[string]Kind{at("o", "f"): File, at("v"): Folder} {
		if _, err := walk(p, k, Who{Apart: []string{at("w")}}, apartHost(d, ids, new(int))); err != nil {
			t.Errorf("%s, beside the apart folder: %v, and want a pass", p, err)
		}
	}
}

// What it costs when this fails: an apart folder given relative is read from wherever the program
// stands, or one that cannot be read lets the walk pass with nothing to keep it apart from.
func TestAnApartFolderThatIsNotAbsoluteOrCannotBeReadIsAnErrorNeverAPass(t *testing.T) {
	t.Parallel()
	d := disk(map[string]node{at("f"): the(file(meID, 0o644), 2)})
	looked := 0
	for _, p := range []string{"w", filepath.Join("a", "b"), at("a") + string(filepath.Separator) + ".." + string(filepath.Separator) + "b", ""} {
		reads := 0
		h := apartHost(d, map[string]uint64{}, &reads)
		look := h.look
		h.look = func(p string) (node, error) { looked++; return look(p) }
		_, err := walk(at("f"), File, Who{Apart: []string{p}}, h)
		wantRefused(t, "the apart folder "+p, err, refusal{nil, Fix{}, "the folders named to be kept apart hold " + strconv.Quote(p) + ", which is not an absolute, clean path"})
		if reads != 0 || looked != 0 {
			t.Errorf("the apart folder %q: %d reads and %d names looked at before it was refused, and want none", p, reads, looked)
		}
	}
	_, err := walk(at("f"), File, Who{Apart: []string{at("gone")}}, apartHost(d, map[string]uint64{}, new(int)))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("an apart folder that is not there: %v, and want fs.ErrNotExist", err)
	}
}

// onDevice is n on the device vol.
func onDevice(n node, vol uint64) node {
	n.id.vol = vol
	return n
}

// A .. after a link in a link's target goes up from where the link leads, as the system goes on
// Unix, and is judged there: a link l to d/../x, with d a link to b/c, is b's x, of another
// account's, and refused. On Windows the system takes the target by its spelling, against l's
// folder, so l is a's own x, and passes. A target ending in .. answers the folder it goes up to.
//
// What it costs when this fails: trust vouches for a's x while the program opens b's, which
// another account wrote.
func TestADotDotAfterALinkIsJudgedWhereTheSystemGoes(t *testing.T) {
	t.Parallel()
	sep := string(filepath.Separator)
	nodes := map[string]node{
		at("a"): dir(rootID, 0o755), at("a", "x"): file(meID, 0o644), at("a", "d"): link(meID),
		at("a", "l"): link(meID), at("a", "abs"): link(meID), at("a", "up"): link(meID), at("a", "dot"): link(meID),
		at("b"): dir(rootID, 0o755), at("b", "c"): dir(rootID, 0o755), at("b", "x"): file(otherID, 0o644),
	}
	d := disk(nodes)
	d.links[at("a", "d")] = at("b", "c")
	d.links[at("a", "l")] = "d" + sep + ".." + sep + "x"
	d.links[at("a", "abs")] = at("a", "d") + sep + ".." + sep + "x"
	d.links[at("a", "up")] = "d" + sep + ".."
	d.links[at("a", "dot")] = "d" + sep + "." + sep + ".." + sep + "." + sep
	for _, l := range []string{at("a", "l"), at("a", "abs")} {
		got, err := walk(l, File, Who{}, d.host(meID))
		if bySpelling {
			if err != nil || got != at("a", "x") {
				t.Errorf("%s on Windows: %q, %v, and want %q, by its spelling", l, got, err, at("a", "x"))
			}
			continue
		}
		wantRefused(t, l, err, refusal{ErrNotOwners, Fix{Path: at("b", "x"), Owner: "uid:0"}, "is owned by " + otherID})
	}
	want := at("b")
	if bySpelling {
		want = at("a")
	}
	for _, l := range []string{at("a", "up"), at("a", "dot")} {
		if got, err := walk(l, Folder, Who{}, d.host(meID)); err != nil || got != want {
			t.Errorf("%s: %q, %v, and want %q", l, got, err, want)
		}
	}
	// .. at the top stays there, as the system's does.
	d.links[at("a", "up")] = strings.Repeat(".."+sep, 4) + "a"
	if got, err := walk(at("a", "up"), Folder, Who{}, d.host(meID)); err != nil || got != at("a") {
		t.Errorf("a .. past the top: %q, %v, and want %q", got, err, at("a"))
	}
}

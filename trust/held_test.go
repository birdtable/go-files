package trust

import (
	"errors"
	"io/fs"
	"os"
	"strconv"
	"testing"
)

// heldHost is a host whose descriptor reads answer n, and whose access-list mask is mask, with
// me as this account and the fake host's root.
func heldHost(n node, mask func(*os.File) (bool, error)) host {
	h := disk(nil).host(meID)
	h.stat = func(*os.File) (node, error) { return n, nil }
	h.mask = mask
	return h
}

// anyFile is a file the test holds open, whose own facts no case reads: the host answers them.
func anyFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func masked(b bool) func(*os.File) (bool, error) {
	return func(*os.File) (bool, error) { return b, nil }
}

// What it costs when this fails: a program that judges a file or folder it already holds, as
// a sandbox its base folder, passes one another account may write, or one of the wrong
// kind, because the judge by handle skips a rule Path keeps.
func TestHeldJudgesTheFileItHoldsByEachRulePathKeepsForTheLastName(t *testing.T) {
	t.Parallel()
	f := anyFile(t)
	ours := Who{Group: func(g uint32) bool { return g == 20 }}
	group := func(perm fs.FileMode) node { n := file(meID, perm); n.gid = 20; return n }
	mounted := file(meID, 0o644)
	mounted.mounter, mounted.mountedOn = otherID, "/Volumes/x"
	ignoring := dir(meID, 0o755)
	ignoring.ownersIgnored = "/Volumes/x"
	everyone := node{owner: userSID, dacl: &dacl{aces: []ace{{typ: aceAllow, mask: 0x2, sid: evilSID}}}}
	null := node{typ: fs.ModeDir, owner: userSID, dacl: &dacl{null: true}}
	for _, c := range []struct {
		name string
		n    node
		k    Kind
		who  Who
		mask func(*os.File) (bool, error)
		me   string
		want refusal
	}{
		{name: "a file of this account's", n: file(meID, 0o644), k: File},
		{name: "a folder of root's", n: dir(rootID, 0o755), k: Folder},
		{name: "a folder as a private folder", n: dir(meID, 0o700), k: PrivateFolder},
		{name: "a folder as a file", n: dir(meID, 0o755), k: File, want: refusal{want: ErrNotPlain, says: "a folder"}},
		{name: "a file as a folder", n: file(meID, 0o644), k: Folder, want: refusal{want: ErrNotPlain, says: "not a folder"}},
		{name: "another's file", n: file(otherID, 0o644), k: File, want: refusal{want: ErrNotOwners, says: otherID}},
		{name: "this account's with RootOnly", n: file(meID, 0o644), k: File, who: Who{RootOnly: true}, want: refusal{want: ErrNotOwners, says: "Path gives the name's"}},
		{name: "a folder others may write", n: dir(meID, 0o757), k: Folder, want: refusal{want: ErrOthersWrite, says: "others"}},
		{name: "root's sticky folder, the name held being last", n: dir(rootID, 0o1777), k: Folder, want: refusal{want: ErrOthersWrite, says: "Path gives the name's"}},
		{name: "a file its group may write, no Group", n: group(0o664), k: File, mask: masked(false), want: refusal{want: ErrOthersWrite, says: "its group"}},
		{name: "a file its group may write, Group lets it in", n: group(0o664), k: File, who: ours, mask: masked(false)},
		{name: "a file its group may write, its bits a mask", n: group(0o664), k: File, who: ours, mask: masked(true), want: refusal{want: ErrOthersWrite, says: "mask"}},
		{name: "a file its group may write, its mask unread", n: group(0o664), k: File, who: ours, want: refusal{want: ErrOthersWrite, says: "mask"}},
		{name: "a private folder its group may read", n: dir(meID, 0o750), k: PrivateFolder, want: refusal{want: ErrOthersRead, says: "0750"}},
		{name: "a program its owner cannot run", n: file(meID, 0o644), k: Program, want: refusal{says: "executed"}},
		{name: "a file on a volume another mounted", n: mounted, k: File, want: refusal{want: ErrNotOwners, says: otherID}},
		{name: "a folder on a volume that ignores owners", n: ignoring, k: Folder, want: refusal{want: ErrOwnersIgnored, says: "ignores ownership"}},
		{name: "a Windows file another account may write", n: everyone, k: File, me: userSID, want: refusal{want: ErrOthersWrite, says: evilSID}},
		{name: "a Windows folder with a null list", n: null, k: Folder, me: userSID, want: refusal{want: ErrOthersWrite, says: "null"}},
	} {
		h := heldHost(c.n, c.mask)
		if c.me != "" {
			h.me = c.me
		}
		wantRefused(t, c.name, held(f, c.k, c.who, h), c.want)
	}
}

// What it costs when this fails: an error reading the held file's access list passes the file as
// one with none, or a host that cannot judge, a closed file or no file at all is a pass.
func TestWhatHeldCannotReadIsAnErrorNeverAPass(t *testing.T) {
	t.Parallel()
	f := anyFile(t)
	broken := errors.New("the mask cannot be read")
	n := file(meID, 0o664)
	err := held(f, File, Who{Group: func(uint32) bool { return true }}, heldHost(n, func(*os.File) (bool, error) { return false, broken }))
	if !errors.Is(err, broken) {
		t.Errorf("an unreadable mask: %v, and want its error", err)
	}
	h := heldHost(file(meID, 0o644), nil)
	h.broken = refuse("", "this account cannot be read", Fix{}, nil)
	if err := held(f, File, Who{}, h); !errors.Is(err, h.broken) {
		t.Errorf("a broken host: %v, and want its refusal", err)
	}
	if err := held(nil, File, Who{}, heldHost(file(meID, 0o644), nil)); err == nil {
		t.Error("no file: a pass, and want an error")
	}
	h = heldHost(node{}, nil)
	h.stat = func(*os.File) (node, error) { return node{}, fs.ErrClosed }
	if err := held(f, File, Who{}, h); !errors.Is(err, fs.ErrClosed) {
		t.Errorf("a file whose descriptor cannot be read: %v, and want its error", err)
	}
	if err := Held(f, Folder, Who{Overflow: true}); !errors.Is(err, ErrOverflow) {
		t.Errorf("Who.Overflow outside a Linux namespace that maps no root: %v, and want ErrOverflow", err)
	}
	if b, err := ReadOwned(f.Name(), "uid:0", false, 1, Who{}, Who{Overflow: true}); b != nil || !errors.Is(err, ErrOverflow) {
		t.Errorf("ReadOwned with Who.Overflow in its second Who, outside a Linux namespace that maps no root: %q, %v, and want ErrOverflow, nothing read", b, err)
	}
	if err := held(f, Folder, Who{Overflow: true}, heldHost(dir(meID, 0o755), nil)); !errors.Is(err, ErrOverflow) || !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Who.Overflow on a host with no overflow account, as off Linux: %v, and want ErrOverflow matching errors.ErrUnsupported", err)
	}
}

// What it costs when this fails: a held file that is a folder of Who.Apart passes, as a
// sandbox base would inside the work tree it is kept out of; or one whose name is swapped at each
// read on macOS is judged on another file's list.
func TestHeldRefusesAnApartFolderAndANameReplacedAtEachTry(t *testing.T) {
	t.Parallel()
	f := anyFile(t)
	n := dir(meID, 0o755)
	n.id = fileID{vol: 1, idx: 2}
	h := heldHost(n, nil)
	h.ident = func(string) (fileID, error) { return fileID{vol: 1, idx: 2}, nil }
	err := held(f, Folder, Who{Apart: []string{at("work")}}, h)
	wantRefused(t, "the folder held is apart", err, refusal{want: ErrInside, says: at("work")})
	h.ident = func(string) (fileID, error) { return fileID{vol: 1, idx: 3}, nil }
	if err := held(f, Folder, Who{Apart: []string{at("work")}}, h); err != nil {
		t.Errorf("another folder apart: %v", err)
	}
	if err := held(f, Folder, Who{Apart: []string{"work"}}, h); err == nil {
		t.Error("an Apart folder that is not absolute: a pass, and want a refusal")
	}
	for _, swaps := range []int{1, tries - 1, tries} {
		stats := 0
		h := heldHost(n, nil)
		h.stat = func(*os.File) (node, error) {
			if stats++; stats <= swaps {
				return node{}, errMoved
			}
			return n, nil
		}
		err := held(f, Folder, Who{}, h)
		name := strconv.Itoa(swaps) + " swaps"
		if swaps < tries {
			wantRefused(t, name, err, refusal{})
		} else {
			wantRefused(t, name, err, refusal{want: ErrReplaced, says: f.Name()})
		}
		if want := min(swaps+1, tries); stats != want {
			t.Errorf("%s: the descriptor was read %d times, and want %d", name, stats, want)
		}
	}
}

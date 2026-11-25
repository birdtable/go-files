package trust

import (
	"cmp"
	"encoding/binary"
	hexes "encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// guidOf is the 16-byte GUID whoOf names who.
func guidOf(t *testing.T, who string) []byte {
	t.Helper()
	kind, id, _ := strings.Cut(who, ":")
	if kind == "guid" {
		g, err := hexes.DecodeString(strings.ReplaceAll(id, "-", ""))
		if err != nil || len(g) != 16 {
			t.Fatalf("%q is not a GUID: %v", who, err)
		}
		return g
	}
	n, err := strconv.ParseUint(id, 10, 32)
	if err != nil || kind != "uid" && kind != "gid" {
		t.Fatalf("%q is not an account or a group", who)
	}
	prefix := userGUID
	if kind == "gid" {
		prefix = groupGUID
	}
	return binary.BigEndian.AppendUint32([]byte(prefix), uint32(n))
}

// filesec is a kauth_filesec holding aces; nil is one that holds no list.
func filesec(t *testing.T, aces []macACE) []byte {
	t.Helper()
	le := binary.LittleEndian
	b := le.AppendUint32(nil, filesecMagic)
	b = append(b, make([]byte, 32)...)
	count := uint32(filesecNoACL)
	if aces != nil {
		count = uint32(len(aces))
	}
	b = le.AppendUint32(le.AppendUint32(b, count), 0)
	for _, e := range aces {
		b = le.AppendUint32(le.AppendUint32(append(b, guidOf(t, e.who)...), e.flags), e.rights)
	}
	return b
}

// answer is getattrlist's answer to macAttrs for the device dev and the inode ino, holding the
// filesec sec when it is not nil, laid out as the kernel lays it.
func answer(dev uint32, ino uint64, sec []byte) []byte {
	le := binary.LittleEndian
	got := uint32(unix.ATTR_CMN_RETURNED_ATTRS | unix.ATTR_CMN_DEVID | unix.ATTR_CMN_FILEID)
	if sec != nil {
		got |= unix.ATTR_CMN_EXTENDED_SECURITY
	}
	b := le.AppendUint32(make([]byte, 4), got)
	b = le.AppendUint32(append(b, make([]byte, 16)...), dev)
	if sec != nil {
		b = le.AppendUint32(le.AppendUint32(b, 16), uint32(len(sec))) // past the reference and the inode
	}
	b = append(le.AppendUint64(b, ino), sec...)
	le.PutUint32(b, uint32(len(b)))
	return b
}

// What it costs when this fails: the darwin host reads its own answer otherwise than the kernel
// lays it out, so an entry another account's is read as this account's, or none at all.
func TestTheMacAnswerIsReadAsTheKernelLaysItOut(t *testing.T) {
	t.Parallel()
	aces := []macACE{allowMac(staffID, macWriteData), denyMac(everyoneID, macDelete), allowMac(someGUID, macReadData), allowMac(rootID, 1<<21)}
	cases := []struct {
		name string
		sec  []byte
		want []macACE
	}{
		{"no list", nil, nil},
		{"a filesec that holds no list", filesec(t, nil), []macACE{}},
		{"a list of four", filesec(t, aces), aces},
	}
	for _, c := range cases {
		dev, ino, got, err := parseAnswer(answer(0x100000e, 0x54ce2670, c.sec))
		if err != nil || dev != 0x100000e || ino != 0x54ce2670 || fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%s: read %#x, %#x, %v, %v, and want %#x, %#x, %v", c.name, dev, ino, got, err, 0x100000e, 0x54ce2670, c.want)
		}
	}
	if macBuf < 24+4+8+8+filesecHead+128*macACESize {
		t.Errorf("an answer of %d bytes holds no list of KAUTH_ACL_MAX_ENTRIES", macBuf)
	}
}

// What it costs when this fails: an answer the darwin host cannot read, cut short, holding
// another attribute or a list it cannot bound, is read as no list, and a name another account may
// write passes (fail-closed).
func TestAMacAnswerThatCannotBeReadIsRefused(t *testing.T) {
	t.Parallel()
	le := binary.LittleEndian
	sec := filesec(t, []macACE{allowMac(staffID, macWriteData)})
	with := func(b []byte, at int, v uint32) []byte {
		c := append([]byte(nil), b...)
		le.PutUint32(c[at:], v)
		return c
	}
	good := answer(1, 2, sec)
	cases := []struct {
		name, says string
		b          []byte
	}{
		{"shorter than its header", "shorter than its header", good[:20]},
		{"longer than it was read", "says it holds", with(good, 0, uint32(len(good)+1))},
		{"a length short of its header", "says it holds", with(good, 0, 12)},
		{"another attribute", "not those asked", with(good, 4, le.Uint32(good[4:])|unix.ATTR_CMN_NAME)},
		{"no inode", "not those asked", with(good, 4, le.Uint32(good[4:])&^unix.ATTR_CMN_FILEID)},
		{"no device", "not those asked", with(good, 4, le.Uint32(good[4:])&^unix.ATTR_CMN_DEVID)},
		{"a volume's attribute", "not those asked", with(good, 8, 1)},
		{"a file's attribute", "not those asked", with(good, 16, 1)},
		{"ended before its attributes", "ends before its attributes", with(good[:40], 0, 40)},
		{"a list reached back into the header", "lie outside", with(good, 28, 0)},
		{"a list past its end", "lie outside", with(good, 32, uint32(len(sec)+1))},
		{"a list far past its end", "lie outside", with(good, 28, 0xffffffff)},
		{"a list shorter than its header", "shorter than its header", with(good, 32, 40)},
		{"a list with another magic", "not the magic", with(good, 44, 0x012cc16e)},
		{"a list of more entries than it holds", "entries take", with(good, 44+36, 2)},
		{"a list of fewer entries than it holds", "entries take", with(good, 44+36, 0)},
		{"a list that holds no list, with an entry", "entries take", with(good, 44+36, filesecNoACL)},
	}
	for _, c := range cases {
		n, err := listed(file(meID, 0o644), c.b, 1, 2)
		if err != nil {
			t.Fatalf("%s: %v, and want the list marked unread", c.name, err)
		}
		err = judgeMac(n, "/p", 0, false, func(macACE) bool { return true })
		wantRefused(t, c.name, err, refusal{ErrOthersWrite, Fix{}, c.says})
		if !errors.Is(err, errors.ErrUnsupported) || n.mac != nil {
			t.Errorf("%s: %v, holding %v, does not match errors.ErrUnsupported or holds entries", c.name, err, n.mac)
		}
	}
}

// What it costs when this fails: an entry for an account the walk trusts is read as another's,
// or one for another account, or a group, as one it trusts.
func TestEachGUIDOfAMacListIsNamedAsItsAccountOrGroup(t *testing.T) {
	t.Parallel()
	for _, who := range []string{rootID, meID, "uid:4294967295", staffID, "gid:0", someGUID, "guid:FFFFEEEE-DDDD-CCCC-BBBB-AAAB00000000", "guid:ABCDEFAB-CDEF-ABCD-EFAB-CDEE00000014"} {
		if got := whoOf(guidOf(t, who)); got != who {
			t.Errorf("the GUID of %s is named %s", who, got)
		}
	}
}

// setList sets the access list of the one name p, following no link, to aces; nil clears it. A
// list set is cleared when the test ends, before its folder is removed, as an entry denying
// everyone its deletion would keep it.
func setList(t *testing.T, p string, aces []macACE) {
	t.Helper()
	if err := writeList(t, p, aces); err != nil {
		t.Fatalf("set the access list of %s: %v", p, err)
	}
	if aces != nil {
		t.Cleanup(func() {
			if err := writeList(t, p, nil); err != nil {
				t.Errorf("clear the access list of %s: %v", p, err)
			}
		})
	}
}

func writeList(t *testing.T, p string, aces []macACE) error {
	t.Helper()
	sec := filesec(t, aces)
	buf := binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, 8), uint32(len(sec)))
	al := unix.Attrlist{Bitmapcount: unix.ATTR_BIT_MAP_COUNT, Commonattr: unix.ATTR_CMN_EXTENDED_SECURITY}
	return unix.Setattrlist(p, &al, append(buf, sec...), unix.FSOPT_NOFOLLOW)
}

// applyMacFix applies fix as a caller would: it takes every entry of fix.Revoke but its deny
// entries off the list of fix.Path itself, read as Path reads it, a link's own for a link, and
// keeps the rest.
func applyMacFix(t *testing.T, fix Fix, path string) {
	t.Helper()
	if fix.Path != path || fix.Revoke == "" || fix != (Fix{Path: fix.Path, Revoke: fix.Revoke}) {
		t.Fatalf("the fix %+v is not a Revoke on %s alone", fix, path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	nd, err := listOf(node{typ: info.Mode().Type()}, path, info)
	if err != nil {
		t.Fatalf("the list of %s: %v", path, err)
	}
	kept := slices.DeleteFunc(slices.Clone(nd.mac), func(e macACE) bool {
		return e.who == fix.Revoke && e.flags&macKindMask != macDeny
	})
	if len(kept) == len(nd.mac) {
		t.Fatalf("the list of %s, %v, holds no entry of %s to revoke", path, nd.mac, fix.Revoke)
	}
	setList(t, path, kept)
}

// What it costs when this fails: on a Mac, a file, key or folder whose access list lets another
// account write it passes Path or ReadOwned, and that account changes what the program runs or
// reads; or its fix, run, leaves it refused.
func TestAMacListOnThisDiskIsRefusedAndPassesOnceItsFixIsRun(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	staff := "gid:" + strconv.Itoa(os.Getgid())
	cases := []struct {
		name  string
		k     Kind
		perm  fs.FileMode
		aces  []macACE
		want  error
		entry string
	}{
		{"its group may write a file", File, 0o644, []macACE{allowMac(staff, macWriteData)}, ErrOthersWrite, staff},
		{"another account may append to a file", File, 0o644, []macACE{allowMac(otherID, 1<<5)}, ErrOthersWrite, otherID},
		{"its group may add a file to a folder", Folder, 0o755, []macACE{denyMac(everyoneID, macDelete), allowMac(staff, macWriteData)}, ErrOthersWrite, staff},
		{"its group may read a key", Key, 0o600, []macACE{allowMac(staff, macReadData)}, ErrOthersRead, staff},
		{"its group may search a private folder", PrivateFolder, 0o700, []macACE{allowMac(staff, macExecute)}, ErrOthersRead, staff},
		{"an account by its own GUID may change a program's list", Program, 0o755, []macACE{allowMac(someGUID, 1<<12)}, ErrOthersWrite, someGUID},
	}
	for i, c := range cases {
		p := filepath.Join(d, "n"+strconv.Itoa(i))
		mk(t, p, c.k, c.perm)
		setList(t, p, c.aces)
		_, err := Path(p, c.k, Who{NoLinks: true})
		u, ok := errors.AsType[*Unsafe](err)
		if !ok || !errors.Is(err, c.want) || !strings.Contains(u.Observed, p) || !strings.Contains(u.Observed, c.entry) {
			t.Errorf("%s: %v, and want an *Unsafe matching %v naming %s and %s", c.name, err, c.want, p, c.entry)
			continue
		}
		applyMacFix(t, u.Fix, p)
		if _, err := Path(p, c.k, Who{NoLinks: true}); err != nil {
			t.Errorf("%s: after %+v, %v", c.name, u.Fix, err)
		}
	}
	p := filepath.Join(d, "home")
	mk(t, p, Folder, 0o755)
	setList(t, p, []macACE{denyMac(everyoneID, macDelete)})
	if _, err := Path(p, Folder, Who{NoLinks: true}); err != nil {
		t.Errorf("a folder everyone is denied deleting, as a home folder is: %v", err)
	}
	setList(t, p, []macACE{allowMac(staff, macWriteData)})
	if _, err := Path(p, Folder, Who{NoLinks: true}); !errors.Is(err, ErrOthersWrite) {
		t.Errorf("a folder its group may add to: %v, and want a refusal matching %v", err, ErrOthersWrite)
	}
	setList(t, p, nil)
	if _, err := Path(p, Folder, Who{NoLinks: true}); err != nil {
		t.Errorf("a folder whose list was cleared: %v", err)
	}
}

// What it costs when this fails: a link whose own access list lets another account change it is
// followed on a Mac, or the list of the file it points to is judged in its place.
func TestALinkWhoseMacListLetsAnotherChangeItIsRefusedOnThisDisk(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	f, l := filepath.Join(d, "f"), filepath.Join(d, "l")
	mk(t, f, File, 0o644)
	if err := os.Symlink("f", l); err != nil {
		t.Fatal(err)
	}
	setList(t, l, []macACE{allowMac(otherID, macWriteData)})
	_, err := Path(l, File, Who{})
	wantRefused(t, "the link's own list", err, refusal{ErrOthersWrite, Fix{Path: l, Revoke: otherID}, "lets " + otherID})
	if _, err := Path(f, File, Who{}); err != nil {
		t.Errorf("the file the link points to took its list: %v", err)
	}
	applyMacFix(t, Fix{Path: l, Revoke: otherID}, l)
	if got, err := Path(l, File, Who{}); err != nil || got != f {
		t.Errorf("after the fix: %q, %v, and want %q", got, err, f)
	}
}

// What it costs when this fails: ReadOwned on a Mac reads a file whose access list lets another
// account write it, or read it when private.
func TestReadOwnedRefusesAFileWhoseMacListLetsAnotherInOnThisDisk(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	me, err := Account()
	if err != nil {
		t.Fatal(err)
	}
	staff := "gid:" + strconv.Itoa(os.Getgid())
	cases := []struct {
		name    string
		aces    []macACE
		private bool
		want    error
	}{
		{"its group may write", []macACE{allowMac(staff, macWriteData)}, false, ErrOthersWrite},
		{"its group may read, private", []macACE{allowMac(staff, macReadData)}, true, ErrOthersRead},
		{"its group may read, not private", []macACE{allowMac(staff, macReadData)}, false, nil},
		{"its owner may write", []macACE{allowMac(me, macWriteData)}, true, nil},
	}
	for i, c := range cases {
		p := filepath.Join(d, "r"+strconv.Itoa(i))
		mk(t, p, File, 0o600)
		setList(t, p, c.aces)
		b, err := ReadOwned(p, me, c.private, 10)
		switch {
		case c.want == nil && (err != nil || string(b) != "x"):
			t.Errorf("%s: %q, %v, and want x", c.name, b, err)
		case c.want != nil && (!errors.Is(err, c.want) || b != nil):
			t.Errorf("%s: %q, %v, and want a refusal matching %v", c.name, b, err, c.want)
		}
	}
}

// listByName is true on macOS: ReadOwned reads the opened file's access list by its name, and
// opens the name again when it names another file by then.
const listByName = true

// What it costs when this fails: a name replaced by rename between its lstat and the read of its
// list is judged by the list of another file, or refused as a list that cannot be read, which a
// second call would pass.
func TestAMacAnswerForAnotherFileIsReadAgainNotJudged(t *testing.T) {
	t.Parallel()
	sec := filesec(t, []macACE{allowMac(staffID, macWriteData)})
	for _, c := range []struct {
		name string
		b    []byte
	}{{"another inode", answer(1, 3, sec)}, {"another device", answer(2, 2, sec)}} {
		if n, err := listed(file(meID, 0o644), c.b, 1, 2); !errors.Is(err, errMoved) || n.mac != nil || n.macUnread != "" {
			t.Errorf("%s: %v, holding %v and %q, and want errMoved and nothing judged", c.name, err, n.mac, n.macUnread)
		}
	}
}

// swapReads is how many times the test below reads a file its owner keeps replacing, with
// ReadOwned and with Path each.
const swapReads = 2000

// What it costs when this fails: a program reading a file it, or its own other process, replaces
// by rename, as write.File and lock do, is refused as though another account may write the file,
// with no way to tell a second try would pass.
func TestAFileItsOwnerKeepsReplacingByRenamePassesEachRead(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	p, next := filepath.Join(d, "settings"), filepath.Join(d, "settings.next")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	me, err := Account()
	if err != nil {
		t.Fatal(err)
	}
	stop, swaps := make(chan struct{}), make(chan int)
	go func() {
		n := 0
		defer func() { swaps <- n }()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := os.WriteFile(next, []byte("x"), 0o600); err != nil {
				t.Error(err)
				return
			}
			if err := os.Rename(next, p); err != nil {
				t.Error(err)
				return
			}
			n++
		}
	}()
	var read, walked int
	var first error
	for range swapReads {
		if _, err := ReadOwned(p, me, true, 10); err != nil {
			read, first = read+1, cmp.Or(first, err)
		}
		if _, err := Path(p, File, Who{NoLinks: true}); err != nil {
			walked, first = walked+1, cmp.Or(first, err)
		}
	}
	close(stop)
	n := <-swaps
	if read+walked != 0 {
		t.Errorf("of %d reads each, while the file was replaced %d times, ReadOwned refused %d and Path %d, the first %v; and want none",
			swapReads, n, read, walked, first)
	}
	if n == 0 {
		t.Error("the file was never replaced, so the reads met no swap")
	}
}

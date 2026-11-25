//go:build windows

package trust

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The six Windows rules on real names, under the ordinary account that runs the tests. Each list
// is set with SetNamedSecurityInfo and each junction with FSCTL_SET_REPARSE_POINT, where a person
// runs icacls and mklink /J: the module's lint keeps os/exec out of this package.

// everyone is the SID of Everyone, the account each rule's list lets in.
const everyone = "S-1-1-0"

// me is the SID of the account that runs the tests, which must be read.
func me(t *testing.T) string {
	t.Helper()
	s, err := self()
	if err != nil {
		t.Fatalf("the SID of the account that runs the tests: %v", err)
	}
	return s
}

// nativeDir is a new folder of the test that passes the walk, links refused: the folder lockedDir
// makes.
func nativeDir(t *testing.T) string {
	t.Helper()
	d := lockedDir(t)
	if _, err := Path(d, Folder, Who{NoLinks: true}); err != nil {
		t.Fatalf("the test's own folder %s does not pass, and every test here needs one that does: %v", d, err)
	}
	return d
}

// lockedDir makes a folder right in the top of the system drive, removed when the test ends, made
// with a list of its own that inherits nothing and lets in this account, SYSTEM and Administrators
// alone, each in full, on the folder and on what it will hold. The walk passes the top of the
// system drive as a folder above the last name, since every other account may only add a folder to
// it. t.TempDir does not do: on an ordinary account %TEMP% is under AppData, which an app's
// capability SID may change on some machines, and the top of another drive lets Authenticated
// Users change it.
func lockedDir(t *testing.T) string {
	t.Helper()
	sys, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + me(t) + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if err != nil {
		t.Fatal(err)
	}
	sa := windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(sa))
	top := filepath.VolumeName(sys) + `\`
	for range 10 {
		d := filepath.Join(top, "go-files-trust-test-"+rand.Text())
		name, err := windows.UTF16PtrFromString(d)
		if err != nil {
			t.Fatal(err)
		}
		err = windows.CreateDirectory(name, &sa)
		if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			continue
		}
		if err != nil {
			t.Fatalf("a folder in %s for the test: %v", top, err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(d); err != nil {
				t.Errorf("removing the test's folder: %v", err)
			}
		})
		return d
	}
	t.Fatalf("no free name for a folder in %s", top)
	return ""
}

// newFile writes a file of five bytes at p.
func newFile(t *testing.T, p string) {
	t.Helper()
	if err := os.WriteFile(p, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// entry is one entry of a list to set: allowed or denied, for sid, of the rights mask, with the
// inheritance flags inherit.
type entry struct {
	mode    windows.ACCESS_MODE
	sid     string
	mask    windows.ACCESS_MASK
	inherit uint32
}

// grant and refuseTo are an entry that allows or denies mask to sid on the name itself.
func grant(sid string, mask windows.ACCESS_MASK) entry {
	return entry{mode: windows.GRANT_ACCESS, sid: sid, mask: mask}
}

func refuseTo(sid string, mask windows.ACCESS_MASK) entry {
	return entry{mode: windows.DENY_ACCESS, sid: sid, mask: mask}
}

// setList sets the access list of p to es and nothing else, inheriting nothing, as icacls
// /inheritance:r /grant does.
func setList(t *testing.T, p string, es ...entry) {
	t.Helper()
	setEntries(t, p, windows.PROTECTED_DACL_SECURITY_INFORMATION, es)
}

// setEntries sets the access list of p to es, with the flags flags beside DACL_SECURITY_INFORMATION.
func setEntries(t *testing.T, p string, flags windows.SECURITY_INFORMATION, es []entry) {
	t.Helper()
	ea := make([]windows.EXPLICIT_ACCESS, 0, len(es))
	for _, e := range es {
		sid, err := windows.StringToSid(e.sid)
		if err != nil {
			t.Fatal(err)
		}
		ea = append(ea, windows.EXPLICIT_ACCESS{AccessPermissions: e.mask, AccessMode: e.mode, Inheritance: e.inherit,
			Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_UNKNOWN, TrusteeValue: windows.TrusteeValueFromSID(sid)}})
	}
	acl, err := windows.ACLFromEntries(ea, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|flags, nil, nil, acl, nil); err != nil {
		t.Fatalf("set the access list of %s: %v", p, err)
	}
}

// setOwnList sets the access list of the reparse point at p itself, as icacls /l reaches it, not
// of what it names, to the list sddl writes: "D:PNO_ACCESS_CONTROL" is a null list, and D:P marks
// it protected in the descriptor itself. It writes the list alone on a handle opened for WRITE_DAC
// (SetKernelObjectSecurity). SetSecurityInfo is refused there on Windows 11 with access denied:
// it reads the old descriptor first and spreads inheritance to a folder's children.
func setOwnList(t *testing.T, p, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	h, err := openName(p, windows.WRITE_DAC)
	if err != nil {
		t.Fatal(err)
	}
	err = windows.SetKernelObjectSecurity(h, windows.DACL_SECURITY_INFORMATION, sd)
	if cerr := windows.CloseHandle(h); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatalf("set the access list of %s itself: %v", p, err)
	}
}

// junction makes the empty folder at at a junction to the folder target, as mklink /J does.
func junction(t *testing.T, at, target string) {
	t.Helper()
	if err := os.Mkdir(at, 0o700); err != nil {
		t.Fatal(err)
	}
	substitute, shown := utf16.Encode([]rune(`\??\`+target)), utf16.Encode([]rune(target))
	names := make([]uint16, 0, len(substitute)+len(shown)+2)
	names = append(append(append(append(names, substitute...), 0), shown...), 0)
	b := binary.LittleEndian.AppendUint32(nil, windows.IO_REPARSE_TAG_MOUNT_POINT)
	for _, v := range []int{8 + 2*len(names), 0, 0, 2 * len(substitute), 2 * (len(substitute) + 1), 2 * len(shown)} {
		b = binary.LittleEndian.AppendUint16(b, uint16(v))
	}
	for _, c := range names {
		b = binary.LittleEndian.AppendUint16(b, c)
	}
	h, err := openName(at, windows.GENERIC_WRITE)
	if err != nil {
		t.Fatal(err)
	}
	var n uint32
	err = windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &b[0], uint32(len(b)), nil, 0, &n, nil)
	if cerr := windows.CloseHandle(h); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatalf("make %s a junction to %s: %v", at, target, err)
	}
}

// What it costs when this fails: the walk refuses the folders every program on Windows is
// installed and kept in, or this account's %APPDATA%, where a program keeps its config and an
// AppContainer may write, or passes the top of the system drive, which every account can
// add to.
//
// testkit points USERPROFILE at a folder of the run's own, and the system expands %USERPROFILE% in
// RoamingAppData, so the test sets it to the profile folder the account's token gives first.
func TestThePartCFoldersGiveTheirRecordedVerdictsOnThisSystem(t *testing.T) {
	profile, err := windows.GetCurrentProcessToken().GetUserProfileDirectory()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("USERPROFILE", profile) // serial: the process environment, which KnownFolderPath expands
	pass := []string{profile}
	for _, id := range []*windows.KNOWNFOLDERID{windows.FOLDERID_ProgramFiles, windows.FOLDERID_System, windows.FOLDERID_RoamingAppData} {
		p, err := windows.KnownFolderPath(id, 0)
		if err != nil {
			t.Fatal(err)
		}
		pass = append(pass, p)
	}
	for _, p := range pass {
		if got, err := Path(p, Folder, Who{}); err != nil || got != p {
			t.Errorf("%s: %q, %v, and want it to pass as it is", p, got, err)
		}
	}
	top := filepath.VolumeName(profile) + `\`
	_, err = Path(top, Folder, Who{})
	wantRefused(t, top+" as the last name", err, refusal{want: ErrOthersWrite, fix: removeGrant(top, authed), says: authed})
}

// What it costs when this fails: a program's config is refused wherever Windows lets an
// AppContainer write, or a name another account may write passes because an AppContainer may write
// it too. The SIDs are read from a real list, so this holds entries to the SID's parts.
func TestAnAppContainerMayWriteANameAndNoOtherAccountMay(t *testing.T) {
	t.Parallel()
	g := filepath.Join(nativeDir(t), "g")
	if err := os.Mkdir(g, 0o700); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(g, "f")
	newFile(t, f)
	for _, sid := range []string{capability, allPackages, aPackage} {
		setList(t, g, grant(me(t), full), grant(sid, full))
		setList(t, f, grant(me(t), full), grant(sid, full))
		n, err := look(g)
		if err != nil || !slices.ContainsFunc(n.dacl.aces, func(e ace) bool { return e.sid == sid && e.sandbox }) {
			t.Errorf("%s's list as read: %+v, %v, and want %s in it as an AppContainer's", g, n.dacl, err, sid)
		}
		for _, c := range []struct {
			p string
			k Kind
		}{{g, Folder}, {f, File}, {f, Key}} {
			if got, err := Path(c.p, c.k, Who{}); err != nil || got != c.p {
				t.Errorf("%s as kind %d, %s may write: %q, %v, and want it to pass", c.p, c.k, sid, got, err)
			}
		}
	}
	for _, other := range []string{"S-1-5-32-545", everyone, evilSID, "S-1-15-1-5", "S-1-5-15-3"} {
		setList(t, f, grant(me(t), full), grant(capability, full), grant(other, windows.FILE_GENERIC_WRITE))
		_, err := Path(f, File, Who{})
		wantRefused(t, other+" may write beside an AppContainer", err, refusal{want: ErrOthersWrite, fix: removeGrant(f, other), says: other})
	}
}

// What it costs when this fails (rule 1): a name of this account's passes where only
// Administrators, SYSTEM and TrustedInstaller are to be trusted, as for a service's own files.
func TestRule1AFileOfThisAccountsIsRefusedWhenRootAloneIsTrusted(t *testing.T) {
	t.Parallel()
	p := filepath.Join(nativeDir(t), "f")
	newFile(t, p)
	n, err := look(p)
	if err != nil || n.owner != me(t) {
		t.Fatalf("the owner of a file this account made: %q, %v, and want %s", n.owner, err, me(t))
	}
	if _, err := Path(p, File, Who{}); err != nil {
		t.Errorf("a file of this account's, this account trusted: %v", err)
	}
	err = judge(n, File, place{path: p, last: true}, Who{RootOnly: true}, local())
	wantRefused(t, "the file, root alone trusted", err, refusal{want: ErrNotOwners, fix: Fix{Path: p, Owner: administrators}, says: me(t)})
}

// What it costs when this fails (rule 2): a file Everyone may write passes, and any account on
// the machine changes what the program reads or runs.
func TestRule2AFileEveryoneMayWriteIsRefusedAndPassesWithTheGrantRemoved(t *testing.T) {
	t.Parallel()
	p := filepath.Join(nativeDir(t), "f")
	newFile(t, p)
	setList(t, p, grant(me(t), full), grant(everyone, windows.FILE_GENERIC_WRITE))
	_, err := Path(p, File, Who{})
	wantRefused(t, "Everyone may write", err, refusal{want: ErrOthersWrite, fix: removeGrant(p, everyone), says: everyone})
	setList(t, p, grant(me(t), full))
	if _, err := Path(p, File, Who{}); err != nil {
		t.Errorf("the list as %+v leaves it: %v", removeGrant(p, everyone), err)
	}
}

// What it costs when this fails (rule 3): a folder whose list gives Everyone a right only on what
// it will hold is refused, and every folder made by an installer that sets such a list with it.
func TestRule3AnInheritOnlyEntryGivesTheFolderItselfNothing(t *testing.T) {
	t.Parallel()
	f := filepath.Join(nativeDir(t), "f")
	if err := os.Mkdir(f, 0o700); err != nil {
		t.Fatal(err)
	}
	children := entry{mode: windows.GRANT_ACCESS, sid: everyone, mask: full,
		inherit: windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT | windows.INHERIT_ONLY}
	setList(t, f, grant(me(t), full), children)
	if _, err := Path(f, Folder, Who{}); err != nil {
		t.Errorf("Everyone in full on what the folder holds only: %v", err)
	}
	setList(t, f, grant(me(t), full), grant(everyone, full))
	_, err := Path(f, Folder, Who{})
	wantRefused(t, "the same entry on the folder itself", err, refusal{want: ErrOthersWrite, fix: removeGrant(f, everyone), says: everyone})
}

// What it costs when this fails: a PrivateFolder whose list gives Users read on the files made in
// it alone passes, and each file a program makes there starts with that entry, so another account
// reads it by bypass-traverse; or a CREATOR OWNER entry, which becomes the maker's, is refused.
// The file made in it shows the entry it inherits on its own list.
func TestAPrivateFolderIsRefusedAnEntryItGivesTheFilesMadeInIt(t *testing.T) {
	t.Parallel()
	const users = "S-1-5-32-545"
	for _, c := range []struct {
		sid  string
		mask windows.ACCESS_MASK
		r    refusal
	}{{users, windows.FILE_GENERIC_READ, refusal{ErrOthersRead, Fix{}, "lets " + users + " read it"}}, {creatorOwner, full, refusal{}}} {
		f := filepath.Join(nativeDir(t), "f")
		if err := os.Mkdir(f, 0o700); err != nil {
			t.Fatal(err)
		}
		setList(t, f, grant(me(t), full), entry{mode: windows.GRANT_ACCESS, sid: c.sid, mask: c.mask, inherit: windows.SUB_OBJECTS_ONLY_INHERIT | windows.INHERIT_ONLY})
		made := filepath.Join(f, "made")
		newFile(t, made)
		for _, p := range []struct {
			path string
			k    Kind
		}{{f, PrivateFolder}, {made, Key}} {
			_, err := Path(p.path, p.k, Who{})
			want := c.r
			if want.want != nil {
				want.fix = removeGrant(p.path, users)
			}
			wantRefused(t, c.sid+" on the files made in it, "+p.path, err, want)
		}
	}
}

// What it costs when this fails (rule 4): a file with no access list at all, which lets every
// account do anything with it, passes.
func TestRule4ANullAccessListIsRefusedAndPassesOnceItIsReset(t *testing.T) {
	t.Parallel()
	p := filepath.Join(nativeDir(t), "f")
	newFile(t, p)
	err := windows.SetNamedSecurityInfo(p, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := look(p); err != nil || n.dacl == nil || !n.dacl.null {
		t.Fatalf("a file set to a null list reads as %+v, %v", n.dacl, err)
	}
	_, err = Path(p, File, Who{})
	wantRefused(t, "a null list", err, refusal{want: ErrOthersWrite, fix: Fix{Path: p, Reset: true}, says: "null"})
	setEntries(t, p, windows.UNPROTECTED_DACL_SECURITY_INFORMATION, []entry{grant(me(t), full)})
	if _, err := Path(p, File, Who{}); err != nil {
		t.Errorf("the list inherited again, as /reset leaves it: %v", err)
	}
}

// What it costs when this fails (rule 5): a junction passes as the folder it stands in for, and
// whoever can point it elsewhere chooses what the program reads; or one is followed where none is
// to be.
func TestRule5AJunctionIsALinkFollowedOrRefusedAsAsked(t *testing.T) {
	t.Parallel()
	d := nativeDir(t)
	target, j := filepath.Join(d, "t"), filepath.Join(d, "j")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	newFile(t, filepath.Join(target, "f"))
	junction(t, j, target)
	if n, err := look(j); err != nil || n.typ != fs.ModeSymlink {
		t.Fatalf("the junction reads as type %v, %v, and want a link", n.typ, err)
	}
	if got, err := Path(filepath.Join(j, "f"), File, Who{}); err != nil || got != filepath.Join(target, "f") {
		t.Errorf("through the junction: %q, %v, and want %s", got, err, filepath.Join(target, "f"))
	}
	_, err := Path(filepath.Join(j, "f"), File, Who{NoLinks: true})
	wantRefused(t, "through the junction, no link followed", err, refusal{want: ErrNotPlain, says: j + " is a link"})
	_, err = ReadOwned(j, me(t), false, 5)
	wantRefused(t, "ReadOwned of the junction", err, refusal{want: ErrNotPlain, says: "a link"})
}

// What it costs when this fails (rule 5): a junction of this account's whose own list lets every
// account change it passes, and any account points it elsewhere and chooses what the program
// reads.
func TestRule5AJunctionAnotherAccountMayChangeIsRefusedBeforeItIsFollowed(t *testing.T) {
	t.Parallel()
	d := nativeDir(t)
	target, j := filepath.Join(d, "t"), filepath.Join(d, "j")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(target, "f")
	newFile(t, f)
	junction(t, j, target)
	setOwnList(t, j, "D:PNO_ACCESS_CONTROL")
	_, err := Path(filepath.Join(j, "f"), File, Who{})
	wantRefused(t, "a null list on the junction", err, refusal{want: ErrOthersWrite, fix: Fix{Path: j, Reset: true}, says: j + " has a null access list"})
	setOwnList(t, j, "D:P(A;;FA;;;"+me(t)+")(A;;FA;;;WD)")
	_, err = Path(filepath.Join(j, "f"), File, Who{})
	wantRefused(t, "Everyone in full on the junction", err, refusal{want: ErrOthersWrite, fix: removeGrant(j, everyone), says: j + " lets " + everyone})
	if n, err := look(target); err != nil || n.dacl == nil || slices.ContainsFunc(n.dacl.aces, func(e ace) bool { return e.sid == everyone }) {
		t.Fatalf("the junction's own list reached its target: %+v, %v", n.dacl, err)
	}
	setOwnList(t, j, "D:P(A;;FA;;;"+me(t)+")(A;;FA;;;SY)(A;;FA;;;BA)")
	if got, err := Path(filepath.Join(j, "f"), File, Who{}); err != nil || got != f {
		t.Errorf("the junction with Everyone's entry removed: %q, %v, and want %s", got, err, f)
	}
}

// What it costs when this fails (rule 6): a name whose list the walk cannot read passes, so a
// list made unreadable hides whatever it grants.
func TestRule6ANameWhoseListCannotBeReadIsAnErrorNeverAPass(t *testing.T) {
	t.Parallel()
	p := filepath.Join(nativeDir(t), "f")
	newFile(t, p)
	// OWNER RIGHTS stands for the owner in place of the rights an owner has without asking.
	setList(t, p, refuseTo("S-1-3-4", windows.READ_CONTROL), grant(me(t), full))
	got, err := Path(p, File, Who{})
	if _, ok := errors.AsType[*Unsafe](err); ok || !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Errorf("a list that cannot be read: %q, %v, and want access denied", got, err)
	}
	if _, err := ReadOwned(p, me(t), false, 5); !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Errorf("ReadOwned of a file whose list cannot be read: %v, and want access denied", err)
	}
}

// What it costs when this fails: a path whose volume is spelled \\?\ is refused, or one relative
// to the current folder of a drive, which another part of the program can change, is walked.
func TestADriveLetterPathIsAbsoluteAndADriveRelativeOneIsNot(t *testing.T) {
	t.Parallel()
	d := nativeDir(t)
	long := `\\?\` + d
	if got, err := Path(long, Folder, Who{}); err != nil || got != long {
		t.Errorf("%s: %q, %v, and want it to pass as it is", long, got, err)
	}
	relative := filepath.VolumeName(d) + "x"
	_, err := Path(relative, Folder, Who{})
	wantRefused(t, relative, err, refusal{says: "is not an absolute, clean path"})
}

// What it costs when this fails: ReadOwned on Windows reads a file another account owns, may
// write or, when private, may read, or one over its limit.
func TestReadOwnedOnWindowsReadsOnlyWhatItsOwnerAloneMayChange(t *testing.T) {
	t.Parallel()
	d := nativeDir(t)
	p := filepath.Join(d, "f")
	newFile(t, p)
	if b, err := ReadOwned(p, me(t), true, 5); err != nil || string(b) != "hello" {
		t.Errorf("a private file of this account's: %q, %v", b, err)
	}
	_, err := ReadOwned(p, me(t), true, 4)
	wantRefused(t, "one byte over the limit", err, refusal{want: ErrTooLong, says: "over 4"})
	_, err = ReadOwned(p, administrators, true, 5)
	wantRefused(t, "another owner named", err, refusal{want: ErrNotOwners, says: "not by " + administrators})
	_, err = ReadOwned(d, me(t), true, 5)
	wantRefused(t, "a folder", err, refusal{want: ErrNotPlain, says: "a folder"})
	setList(t, p, grant(me(t), full), grant(everyone, windows.FILE_GENERIC_READ))
	_, err = ReadOwned(p, me(t), true, 5)
	wantRefused(t, "Everyone may read it, private", err, refusal{want: ErrOthersRead, fix: removeGrant(p, everyone), says: everyone})
	if _, err := ReadOwned(p, me(t), false, 5); err != nil {
		t.Errorf("Everyone may read it, not private: %v", err)
	}
	setList(t, p, grant(me(t), full), grant(everyone, windows.FILE_GENERIC_WRITE))
	_, err = ReadOwned(p, me(t), false, 5)
	wantRefused(t, "Everyone may write it", err, refusal{want: ErrOthersWrite, fix: removeGrant(p, everyone), says: everyone})
	setList(t, p, grant(me(t), full), grant(capability, full), grant(allPackages, full))
	if _, err := ReadOwned(p, me(t), true, 5); err != nil {
		t.Errorf("an AppContainer may write and read it, private: %v", err)
	}
}

// What it costs when this fails: an access entry whose size runs past its list, or cuts its SID
// short, is read from the bytes after it, an account the list never named; or the read leaves the
// descriptor's memory. Each entry is a real list's, its size then set by hand.
func TestAnAccessEntryIsReadOnlyWithinItsOwnSizeAndItsList(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		size func(aclSize uint16) uint16
		says string
	}{
		{"as Windows wrote it", nil, ""},
		{"past the list", func(n uint16) uint16 { return n }, "not within the list's"},
		{"shorter than its header", func(uint16) uint16 { return 4 }, "not within the list's"},
		{"its SID cut short", func(uint16) uint16 { return 12 }, "no SID whole"},
	} {
		sd, err := windows.SecurityDescriptorFromString("D:(A;;FA;;;BU)(A;;FA;;;SY)")
		if err != nil {
			t.Fatal(err)
		}
		acl, _, err := sd.DACL()
		if err != nil {
			t.Fatal(err)
		}
		if c.size != nil {
			var a *windows.ACCESS_ALLOWED_ACE
			if err := windows.GetAce(acl, 0, &a); err != nil {
				t.Fatal(err)
			}
			a.Header.AceSize = c.size(binary.LittleEndian.Uint16(unsafe.Slice((*byte)(unsafe.Pointer(acl)), 8)[2:])) //nolint:gosec // G103: the ACL header, 8 bytes
		}
		d, err := entries(sd)
		if c.size == nil {
			if err != nil || len(d.aces) != 2 || d.aces[0].sid != "S-1-5-32-545" || d.aces[1].sid != "S-1-5-18" {
				t.Errorf("%s: %+v, %v, and want S-1-5-32-545 then S-1-5-18", c.name, d, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: %+v, %v, and want an error saying %q", c.name, d, err, c.says)
		}
	}
}

// Account is the SID of the account that runs the tests, and ReadOwned reads a file it made as
// Account's.
//
// What it costs when this fails: a program reading its own settings through ReadOwned as Account
// is refused its own file.
func TestAccountOwnsAFileThisAccountMadeOnThisDisk(t *testing.T) {
	t.Parallel()
	p := filepath.Join(nativeDir(t), "f")
	newFile(t, p)
	got, err := Account()
	if err != nil || got != me(t) {
		t.Fatalf("Account: %q, %v, and want %q", got, err, me(t))
	}
	if b, err := ReadOwned(p, got, true, 5); err != nil || string(b) != "hello" {
		t.Errorf("ReadOwned as %s of a file it made: %q, %v", got, b, err)
	}
}

// What it costs when this fails: a path in the work tree on this disk passes because it was
// spelled through a junction, in another case, which NTFS ignores, or as the tree itself, and a
// head commits a file the tools read; or one beside the tree is refused.
func TestAPathInAnApartFolderOnThisDiskIsRefusedHoweverItIsSpelled(t *testing.T) {
	t.Parallel()
	d := nativeDir(t)
	w, o, j := filepath.Join(d, "work"), filepath.Join(d, "other"), filepath.Join(d, "j")
	for _, p := range []string{w, filepath.Join(w, "sub"), o} {
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	newFile(t, filepath.Join(w, "sub", "f"))
	newFile(t, filepath.Join(o, "f"))
	junction(t, j, w)
	for _, c := range []struct {
		path, apart, at string
		k               Kind
	}{
		{w, w, w, Folder},
		{filepath.Join(w, "sub", "f"), w, w, File},
		{filepath.Join(j, "sub", "f"), w, w, File},
		{filepath.Join(w, "sub", "f"), j, w, File},
		{filepath.Join(d, "WORK", "sub", "f"), w, filepath.Join(d, "WORK"), File},
	} {
		_, err := Path(c.path, c.k, Who{Apart: []string{c.apart}})
		wantRefused(t, c.path+" apart from "+c.apart, err, refusal{ErrInside, Fix{}, c.at + " is " + c.apart + ", a folder named to be kept apart"})
	}
	if got, err := Path(filepath.Join(o, "f"), File, Who{Apart: []string{w}}); err != nil || got != filepath.Join(o, "f") {
		t.Errorf("a file beside the apart folder: %q, %v, and want a pass", got, err)
	}
}

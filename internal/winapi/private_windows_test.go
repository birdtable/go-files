package winapi

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileAll is FA, every right on a file or folder, as Private grants it.
const fileAll = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff

// shareAll is the share of a file write makes: read, write and delete.
const shareAll = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE

// entry is one entry of an access list, read back from the system.
type entry struct {
	kind  uint8
	flags uint8
	mask  windows.ACCESS_MASK
	who   string
}

// listOf is the owner of path, whether its access list is protected from the folder above, and
// the list's entries, read back from the system by name.
//
//nolint:gosec // G103: an entry's SID starts at SidStart, as the system lays it out
func listOf(t *testing.T, path string) (owner string, protected bool, entries []entry) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("read the access list of %s: %v", path, err)
	}
	o, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		t.Fatalf("%s has no access list (%v), which lets every account in", path, err)
	}
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry{ace.Header.AceType, ace.Header.AceFlags, ace.Mask, (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()})
	}
	return o.String(), control&windows.SE_DACL_PROTECTED != 0, entries
}

// me is the SID of the account this test runs as.
func me(t *testing.T) string {
	t.Helper()
	u, err := windows.GetCurrentThreadEffectiveToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return u.User.Sid.String()
}

// lettingUsersIn is a temp folder whose list, protected, grants SYSTEM and this account every
// right and Users read, each to all the folder holds. It fails t unless a file made in it with no
// list of its own inherits the Users entry, so a make that took the folder's list would show it.
func lettingUsersIn(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;" + me(t) + ")(A;OICI;FR;;;BU)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatalf("let Users into %s: %v", dir, err)
	}
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, entries := listOf(t, plain)
	if !slices.ContainsFunc(entries, func(e entry) bool { return e.who == "S-1-5-32-545" && e.flags&windows.INHERITED_ACE != 0 }) {
		t.Fatalf("a plain file in %s inherits %+v, and want Users' entry among them", dir, entries)
	}
	if err := os.Remove(plain); err != nil {
		t.Fatal(err)
	}
	return dir
}

// ownerOnly fails t unless path's list is protected or, for what was made in a private folder,
// inherited, and is two entries granting every right: SYSTEM's and this account's, each with
// flags. Administrators, and any group the folder above lets in, are absent. What was made with
// the list, protected, is this account's too; what another call made in a private folder has the
// owner the system gives, which is Administrators for an elevated test.
func ownerOnly(t *testing.T, path string, protected bool, flags uint8) {
	t.Helper()
	owner, isProtected, got := listOf(t, path)
	want := []entry{
		{windows.ACCESS_ALLOWED_ACE_TYPE, flags, fileAll, "S-1-5-18"},
		{windows.ACCESS_ALLOWED_ACE_TYPE, flags, fileAll, me(t)},
	}
	if protected && owner != me(t) || isProtected != protected || !slices.Equal(got, want) {
		t.Errorf("%s: owner %s, protected %v, list %+v; want owner %s, protected %v, list %+v", path, owner, isProtected, got, me(t), protected, want)
	}
}

// A file MakeFile makes, and a folder MakeFolder makes, is this account's, with a protected list
// that grants SYSTEM and this account alone, though the folder they are made in grants Users read
// to all it holds (lettingUsersIn); a file then made in the private folder by a plain
// os.WriteFile inherits the same two entries.
//
// What keeps the folder's entries out is the list given at the create: made with the owner alone,
// the file and the folder each take the folder's Users entry, and this fails. The list the create
// is given reads back protected and takes nothing from above whether it says P or AR (as a native
// Windows run read it), so P is not what this proves.
//
// What it costs when this fails: a key, a lock or a temp folder made private is readable, or
// writable, by another account on the machine.
func TestWhatIsMadeHasTheOwnerOnlyListAndNothingOfTheFolderAbove(t *testing.T) {
	t.Parallel()
	dir := lettingUsersIn(t)
	open := func(name string) (*os.File, error) { return os.Open(name) }
	f, err := MakeFile(open, filepath.Join(dir, "key"), windows.FILE_GENERIC_WRITE, shareAll, Listless{Err: errors.ErrUnsupported})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("secret")); err != nil {
		t.Errorf("write to the file MakeFile made: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	ownerOnly(t, filepath.Join(dir, "key"), true, 0)
	folder := filepath.Join(dir, "private")
	if err := MakeFolder(open, folder, Listless{Err: errors.ErrUnsupported}); err != nil {
		t.Fatal(err)
	}
	ownerOnly(t, folder, true, windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE)
	if err := os.WriteFile(filepath.Join(folder, "inside"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerOnly(t, filepath.Join(folder, "inside"), false, windows.INHERITED_ACE)
}

// A name there already, a file, a folder or a link, is refused with an error matching
// fs.ErrExist, and is left as it was.
//
// What it costs when this fails: a make writes through a link planted at the name, or answers a
// file someone else made, with their list, as the private one it made.
func TestANameThereAlreadyIsRefusedAsExisting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("theirs"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	names := []string{"file", "folder"}
	if err := os.Symlink(filepath.Join(dir, "elsewhere"), filepath.Join(dir, "link")); err == nil {
		names = append(names, "link")
	} else {
		t.Logf("no link made (%v): this account may not make one, so the link row is not run", err)
	}
	for _, name := range names {
		path := filepath.Join(dir, name)
		if f, err := MakeFile(os.Open, path, windows.FILE_GENERIC_WRITE, shareAll, Listless{Err: errors.ErrUnsupported}); !errors.Is(err, fs.ErrExist) {
			if f != nil {
				_ = f.Close()
			}
			t.Errorf("MakeFile at the %s there: %v, and want fs.ErrExist", name, err)
		}
		if err := MakeFolder(os.Open, path, Listless{Err: errors.ErrUnsupported}); !errors.Is(err, fs.ErrExist) {
			t.Errorf("MakeFolder at the %s there: %v, and want fs.ErrExist", name, err)
		}
	}
	if got, err := os.ReadFile(filepath.Join(dir, "file")); err != nil || string(got) != "theirs" {
		t.Errorf("the file there reads %q, %v, and want it left as it was", got, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "elsewhere")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a make went through the link: %v", err)
	}
}

// On a volume that keeps no access lists, a missing name is refused with the caller's own error,
// matching errors.ErrUnsupported, and nothing is made; a name there already is still answered as
// existing, so a lock or a folder that is there opens as it did.
//
// What it costs when this fails: a key or a lock made on a FAT or exFAT stick is every account's,
// while its program believes it private.
func TestAVolumeWithNoListsRefusesAMakeWithTheCallersErrorAndMakesNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	refuse := Listless{Err: errors.Join(errors.New("no lists here"), errors.ErrUnsupported), Test: true}
	f, err := MakeFile(os.Open, filepath.Join(dir, "key"), windows.FILE_GENERIC_WRITE, shareAll, refuse)
	if f != nil {
		_ = f.Close()
	}
	if !errors.Is(err, refuse.Err) || !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("MakeFile on a volume with no lists: %v, and want the caller's refusal", err)
	}
	if err := MakeFolder(os.Open, filepath.Join(dir, "folder"), refuse); !errors.Is(err, refuse.Err) {
		t.Errorf("MakeFolder on a volume with no lists: %v, and want the caller's refusal", err)
	}
	if left, err := os.ReadDir(dir); err != nil || len(left) != 0 {
		t.Errorf("a refused make left %v (%v), and want nothing", left, err)
	}
	if err := os.Mkdir(filepath.Join(dir, "there"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := MakeFolder(os.Open, filepath.Join(dir, "there"), refuse); !errors.Is(err, fs.ErrExist) {
		t.Errorf("MakeFolder at a folder there, on a volume with no lists: %v, and want fs.ErrExist", err)
	}
}

// A volume that will not say whether it keeps access lists is answered as one that keeps none: a
// missing name is refused with the caller's own error, matching errors.ErrUnsupported, and nothing
// is made.
//
// What it costs when this fails: a program that refuses by errors.Is(err, ErrNoAccessList) where
// its file cannot be made private gets an untyped volume error instead, and takes it for another
// failure.
func TestAVolumeThatWillNotSayWhetherItKeepsListsRefusesAMakeWithTheCallersError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	refuse := Listless{Err: errors.Join(errors.New("no lists here"), errors.ErrUnsupported), fault: volumeUnsaid}
	f, err := MakeFile(os.Open, filepath.Join(dir, "key"), windows.FILE_GENERIC_WRITE, shareAll, refuse)
	if f != nil {
		_ = f.Close()
	}
	if !errors.Is(err, refuse.Err) || !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("MakeFile on a volume that will not say: %v, and want the caller's refusal", err)
	}
	if err := MakeFolder(os.Open, filepath.Join(dir, "folder"), refuse); !errors.Is(err, refuse.Err) {
		t.Errorf("MakeFolder on a volume that will not say: %v, and want the caller's refusal", err)
	}
	if left, err := os.ReadDir(dir); err != nil || len(left) != 0 {
		t.Errorf("a refused make left %v (%v), and want nothing", left, err)
	}
}

// A make the volume refuses for its owner, ERROR_INVALID_OWNER, as a share that knows this account
// by another SID refuses the local one, is refused with the caller's own error, matching
// errors.ErrUnsupported, and nothing is made. The test names an owner no token holds; a process
// with SeRestorePrivilege enabled may name any owner, and no test runs with it enabled.
//
// What it costs when this fails: a program writing to a NAS or a workgroup share gets an untyped
// ERROR_INVALID_OWNER where the package promises ErrNoAccessList, and cannot tell that its file
// could not be made private there.
func TestAMakeTheVolumeRefusesForItsOwnerAnswersTheCallersErrorAndMakesNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	refuse := Listless{Err: errors.Join(errors.New("no lists here"), errors.ErrUnsupported), fault: ownerRefused}
	f, err := MakeFile(os.Open, filepath.Join(dir, "key"), windows.FILE_GENERIC_WRITE, shareAll, refuse)
	if f != nil {
		_ = f.Close()
	}
	if !errors.Is(err, refuse.Err) || !errors.Is(err, windows.ERROR_INVALID_OWNER) {
		t.Errorf("MakeFile with an owner the volume refuses: %v, and want the caller's refusal with ERROR_INVALID_OWNER", err)
	}
	if err := MakeFolder(os.Open, filepath.Join(dir, "folder"), refuse); !errors.Is(err, refuse.Err) {
		t.Errorf("MakeFolder with an owner the volume refuses: %v, and want the caller's refusal", err)
	}
	if left, err := os.ReadDir(dir); err != nil || len(left) != 0 {
		t.Errorf("a refused make left %v (%v), and want nothing", left, err)
	}
}

// A make whose list this account cannot reach by name, to read and to write, is refused with the
// caller's own error, matching errors.ErrUnsupported, with the denial its probe met, and nothing
// is left in the folder, the probe's throwaway name included. One row makes the list grant an
// account no token holds in place of this one, the owner left as this account: NTFS grants the
// make's handle and denies the open by name, as under a restricted token or an AppContainer the
// list does not name, or on a share that keeps the list as sent and knows this account by another
// SID. The other grants this account read alone: the open by name to read is granted and to write
// is denied, as under a write-restricted token whose restricted SIDs the list does not name.
// None of those four is run here. The runner must not be SYSTEM, whom the list
// grants whatever the fault: under SYSTEM both rows fail, with every make made.
//
// What it costs when this fails: a lock, a log or a temp folder is made that its program cannot
// open again, or cannot write, so its next open, the lock's reopen or Append's next call, is
// denied, and a file only SYSTEM reaches, or that its program only reads, is left at the name.
func TestAMakeThisAccountCannotOpenAgainIsRefusedAndLeavesNothing(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name  string
		fault fault
	}{
		{"a list this account is not on", listElsewhere},
		{"a list that grants this account read alone", readOnly},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			refuse := Listless{Err: errors.Join(errors.New("no lists here"), errors.ErrUnsupported), fault: row.fault}
			f, err := MakeFile(os.Open, filepath.Join(dir, "key"), windows.FILE_GENERIC_WRITE, shareAll, refuse)
			if f != nil {
				_ = f.Close()
			}
			if !errors.Is(err, refuse.Err) || !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
				t.Errorf("MakeFile with %s: %v, and want the caller's refusal with ERROR_ACCESS_DENIED", row.name, err)
			}
			// The lock's share: read and write, no delete.
			f, err = MakeFile(os.Open, filepath.Join(dir, "lock"), windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, refuse)
			if f != nil {
				_ = f.Close()
			}
			if !errors.Is(err, refuse.Err) || !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
				t.Errorf("MakeFile sharing no delete, with %s: %v, and want the caller's refusal with ERROR_ACCESS_DENIED", row.name, err)
			}
			if err := MakeFolder(os.Open, filepath.Join(dir, "folder"), refuse); !errors.Is(err, refuse.Err) || !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
				t.Errorf("MakeFolder with %s: %v, and want the caller's refusal with ERROR_ACCESS_DENIED", row.name, err)
			}
			if left, err := os.ReadDir(dir); err != nil || len(left) != 0 {
				t.Errorf("a refused make left %v (%v), and want nothing", left, err)
			}
			// A lock file there already is answered as there, so the lock opens it as before.
			there := filepath.Join(dir, "there")
			if err := os.WriteFile(there, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			f, err = MakeFile(os.Open, there, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, refuse)
			if f != nil {
				_ = f.Close()
			}
			if !errors.Is(err, fs.ErrExist) {
				t.Errorf("MakeFile of a name there already, with %s: %v, and want an error matching fs.ErrExist", row.name, err)
			}
			if left, err := os.ReadDir(dir); err != nil || len(left) != 1 {
				t.Errorf("a make of a name there already left %v (%v), and want that name alone", left, err)
			}
		})
	}
}

// The handle MakeFile answers for a lock file is the create's own, with no DELETE: it shares no
// delete, so a removal is refused while it is held, and another open sharing read and write, as a
// contender's does, is not refused for sharing, as it would be were the handle to DELETE. The
// probe's throwaway name is not left: the folder holds the lock file alone.
//
// What it costs when this fails: of two runs that start together on a lock file not yet made, the
// one that loses the create is told neither that it holds the lock nor that another does, and
// Wait does not wait; or a probe is left beside every file made.
func TestTheHandleMakeFileAnswersHasNoDelete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lock := filepath.Join(dir, "lock")
	f, err := MakeFile(os.Open, lock, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, Listless{Err: errors.ErrUnsupported})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(lock); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Errorf("remove the held lock file: %v, and want a sharing violation, as its handle shares no delete", err)
	}
	// Another open of the lock, sharing read and write as a contender's does, is not refused: the
	// handle answered has no DELETE, which would make every other open share delete.
	name, err := windows.UTF16PtrFromString(lock)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Errorf("open the held lock file sharing read and write: %v, and want it open", err)
	} else if err := windows.CloseHandle(h); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	ownerOnly(t, lock, true, 0)
	if left, err := os.ReadDir(dir); err != nil || len(left) != 1 || left[0].Name() != "lock" {
		t.Errorf("the make left %v (%v), and want the lock file alone", left, err)
	}
}

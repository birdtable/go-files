package confine

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// usersSID is BUILTIN\Users, which every account of the machine is in.
const usersSID = "S-1-5-32-545"

// listAt is the owner of path, whether its access list is protected from the folder above, and the
// list's entries, read back from the system by name.
//
//nolint:gosec // G103: an entry's SID starts at SidStart, as the system lays it out
func listAt(t *testing.T, path string) (owner string, protected bool, entries []entry) {
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

// readableToUsers gives the folder at path a protected list granting SYSTEM and this account every
// right, and BUILTIN\Users read, each to all the folder holds, as a service's shared folder is.
func readableToUsers(t *testing.T, path string) {
	t.Helper()
	u, err := windows.GetCurrentThreadEffectiveToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sddl := "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;" + u.User.Sid.String() + ")(A;OICI;FR;;;BU)"
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

// Each folder MkdirShared makes has no list of its own: its list is unprotected and holds the
// inheritable entries of the folder above alone, BUILTIN\Users' read among them. A folder Mkdir or
// MkdirPrivate makes in the same folder is still the owner-only list.
//
// What it costs when this fails: a service's shared folder is made owner-only, and the accounts it
// is for cannot read it, or Mkdir, asked for a private folder, takes the folder above's grants.
func TestMkdirSharedFolderInheritsTheFolderAbove(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pub := filepath.Join(dir, "pub")
	if err := os.Mkdir(pub, 0o700); err != nil {
		t.Fatal(err)
	}
	readableToUsers(t, pub)
	root := rootAt(t, dir)
	r, err := MkdirShared(root, filepath.Join("pub", "a", "b"))
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	u, err := windows.GetCurrentThreadEffectiveToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{filepath.Join(pub, "a"), filepath.Join(pub, "a", "b")} {
		owner, protected, got := listAt(t, d)
		if owner != u.User.Sid.String() && owner != "S-1-5-32-544" {
			t.Errorf("%s is %s's, and want this account's or Administrators'", d, owner)
		}
		if protected || !slices.ContainsFunc(got, func(e entry) bool { return e.who == usersSID }) ||
			slices.ContainsFunc(got, func(e entry) bool { return e.flags&windows.INHERITED_ACE == 0 }) {
			t.Errorf("%s: protected %v, list %+v; want unprotected, every entry inherited, BUILTIN\\Users' among them", d, protected, got)
		}
	}
	for what, mk := range map[string]func(*os.Root, string) (*os.Root, error){
		"Mkdir":        func(r *os.Root, n string) (*os.Root, error) { return Mkdir(r, n, 0o755) },
		"MkdirPrivate": MkdirPrivate,
	} {
		r, err := mk(root, filepath.Join("pub", what))
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		r.Close()
		ownerOnlyFolder(t, filepath.Join(pub, what))
	}
}

// MkdirShared refuses with ErrNoAccessList a volume that keeps no access lists, and makes nothing;
// a folder it made that is another account's when opened is refused with ErrOutside and left; a
// junction at a component is refused with ErrOutside, and nothing is made through it.
//
// What it costs when this fails: a shared folder on FAT is taken as made with no owner to hold it
// to, or a folder another account swapped in is answered as this one's, or one made through a
// junction lands in another account's tree.
func TestMkdirSharedRefusesListlessAnotherOwnerAndAJunction(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "target"), 0o700); err != nil {
		t.Fatal(err)
	}
	plantJunction(t, filepath.Join(dir, "target"), filepath.Join(dir, "junction"))
	root := rootAt(t, dir)
	none := func(string) {}
	r, err := mkdirAs(root, "listless", 0o755, inherits, none, true)
	if err == nil {
		r.Close()
	}
	if !errors.Is(err, ErrNoAccessList) {
		t.Errorf("MkdirShared on a volume with no access lists: %v, and want ErrNoAccessList", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "listless")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("MkdirShared refused on a volume with no access lists made the folder (%v)", err)
	}
	r, err = mkdirAs(root, "elsewhere", 0o755, inheritsElsewhere, none, false)
	if err == nil {
		r.Close()
	}
	if !errors.Is(err, ErrOutside) {
		t.Errorf("a folder MkdirShared made, another account's when opened: %v, and want ErrOutside", err)
	}
	if fi, err := os.Lstat(filepath.Join(dir, "elsewhere")); err != nil || !fi.IsDir() {
		t.Errorf("the folder of another account is %v (%v), and want it left", fi, err)
	}
	r, err = MkdirShared(root, filepath.Join("junction", "new"))
	if err == nil {
		r.Close()
	}
	if !errors.Is(err, ErrOutside) {
		t.Errorf("MkdirShared through a junction: %v, and want ErrOutside", err)
	}
	if names := namesIn(filepath.Join(dir, "target")); len(names) != 0 {
		t.Errorf("the junction's target holds %q, and want nothing made in it", names)
	}
}

// A folder MkdirShared made, swapped before its open for an empty folder of this account that
// carries a list of its own, is refused with ErrOutside, read from the opened folder's own handle,
// and the folder swapped in is left at the name with its list: one with an explicit entry granting
// BUILTIN\Users every right beside what it inherits, and one whose list is protected from the
// folder above.
//
// What it costs when this fails: an account that may rename in the folder above swaps in an
// empty folder of this account's that grants it, or that keeps out the folder above's grants, and
// MkdirShared answers it as the made one, so who reaches what the caller makes in it is not the
// folder above's say (confine/mkdir.go).
func TestMkdirSharedRefusesAFolderSwappedInWithAListOfItsOwn(t *testing.T) {
	t.Parallel()
	for what, list := range map[string]func(t *testing.T, path string){
		"an explicit entry for BUILTIN\\Users": func(t *testing.T, path string) {
			t.Helper()
			sd, err := windows.SecurityDescriptorFromString("D:(A;OICI;FA;;;" + usersSID + ")")
			if err != nil {
				t.Fatal(err)
			}
			dacl, _, err := sd.DACL()
			if err != nil {
				t.Fatal(err)
			}
			if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
				t.Fatalf("give the folder swapped in an entry of its own: %v", err)
			}
		},
		"a protected list": readableToUsers,
	} {
		dir := t.TempDir()
		made := filepath.Join(dir, "new")
		done := false
		r, err := mkdirAs(rootAt(t, dir), "new", 0o755, inherits, func(step string) {
			if step != stepMadeFolder || done {
				return
			}
			done = true
			if err := os.Remove(made); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(made, 0o700); err != nil {
				t.Fatal(err)
			}
			list(t, made)
		}, false)
		if err == nil {
			r.Close()
		}
		if !done || !errors.Is(err, ErrOutside) {
			t.Errorf("a folder with %s swapped in for one MkdirShared made (reached %v): %v, and want ErrOutside", what, done, err)
		}
		_, protected, got := listAt(t, made)
		if !protected && !slices.ContainsFunc(got, func(e entry) bool { return e.flags&windows.INHERITED_ACE == 0 }) {
			t.Errorf("the folder with %s swapped in has the list %+v, protected %v, and want it left with its own", what, got, protected)
		}
	}
}

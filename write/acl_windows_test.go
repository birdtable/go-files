package write

import (
	"fmt"
	"slices"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileAll is FA, every right on a file, as the owner-only list grants it.
const fileAll = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff

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

// ownerOnly fails t unless path is this account's, with a protected list of two entries granting
// every right: SYSTEM's and this account's, as a file write makes.
func ownerOnly(t *testing.T, path string) {
	t.Helper()
	owner, protected, got := listOf(t, path)
	want := []entry{
		{windows.ACCESS_ALLOWED_ACE_TYPE, 0, fileAll, "S-1-5-18"},
		{windows.ACCESS_ALLOWED_ACE_TYPE, 0, fileAll, me(t)},
	}
	if owner != me(t) || !protected || !slices.Equal(got, want) {
		t.Errorf("%s: owner %s, protected %v, list %+v; want owner %s, protected, list %+v", path, owner, protected, got, me(t), want)
	}
}

// listed gives path the access list sddl says, protected from the folder above: a D: string alone,
// "D:NO_ACCESS_CONTROL" for a NULL list, which lets every account in.
func listed(t *testing.T, path, sddl string) {
	t.Helper()
	if err := setList(path, sddl); err != nil {
		t.Fatal(err)
	}
}

// setList is listed's work, its error the caller's: a volume may refuse a list, as one with an
// object entry.
func setList(path, sddl string) error {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("the list %s: %w", sddl, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("the list %s: %w", sddl, err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("give %s the list %s: %w", path, sddl, err)
	}
	return nil
}

// isReadOnly is whether path has the read-only attribute.
func isReadOnly(t *testing.T, path string) bool {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := windows.GetFileAttributes(name)
	if err != nil {
		t.Fatalf("the attributes of %s: %v", path, err)
	}
	return attrs&windows.FILE_ATTRIBUTE_READONLY != 0
}

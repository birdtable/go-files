package confine

import (
	"slices"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileAll is FA, every right on a file or folder, as the owner-only list grants it.
const fileAll = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff

// entry is one entry of an access list, read back from the system.
type entry struct {
	kind  uint8
	flags uint8
	mask  windows.ACCESS_MASK
	who   string
}

// ownerOnlyFolder fails t unless the folder at path is this account's, with a protected list of
// two entries granting every right, each to all the folder holds: SYSTEM's and this account's,
// as Mkdir makes a folder.
//
//nolint:gosec // G103: an entry's SID starts at SidStart, as the system lays it out
func ownerOnlyFolder(t *testing.T, path string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("read the access list of %s: %v", path, err)
	}
	owner, _, err := sd.Owner()
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
	var got []entry
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			t.Fatal(err)
		}
		got = append(got, entry{ace.Header.AceType, ace.Header.AceFlags, ace.Mask, (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()})
	}
	u, err := windows.GetCurrentThreadEffectiveToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	me := u.User.Sid.String()
	const oici = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
	want := []entry{
		{windows.ACCESS_ALLOWED_ACE_TYPE, oici, fileAll, "S-1-5-18"},
		{windows.ACCESS_ALLOWED_ACE_TYPE, oici, fileAll, me},
	}
	protected := control&windows.SE_DACL_PROTECTED != 0
	if owner.String() != me || !protected || !slices.Equal(got, want) {
		t.Errorf("%s: owner %s, protected %v, list %+v; want owner %s, protected, list %+v", path, owner, protected, got, me, want)
	}
}

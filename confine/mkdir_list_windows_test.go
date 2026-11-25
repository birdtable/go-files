package confine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// A folder Mkdir made, swapped before its open for an empty folder of this account listed
// D:(A;OICI;FA;;;WD), every right to Everyone, is refused with ErrOutside, read from the opened
// folder's own handle, and the folder swapped in is left at the name with its list; with no swap
// the folder answered is this account's, with the protected owner-only list.
//
// What it costs when this fails: a folder another account may write the folder above swaps in,
// open to Everyone, is answered as the private one Mkdir made, and what the caller makes in it
// inherits Everyone's entry.
func TestMkdirRefusesAFolderSwappedInWithAnotherList(t *testing.T) {
	t.Parallel()
	const everyone = "(A;OICI;FA;;;WD)"
	for _, swap := range []bool{true, false} {
		dir := t.TempDir()
		made := filepath.Join(dir, "new")
		done := false
		r, err := mkdir(rootAt(t, dir), "new", 0o700, func(step string) {
			if step != stepMadeFolder || done || !swap {
				return
			}
			done = true
			if err := os.Remove(made); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(made, 0o700); err != nil {
				t.Fatal(err)
			}
			sd, err := windows.SecurityDescriptorFromString("D:" + everyone)
			if err != nil {
				t.Fatal(err)
			}
			dacl, _, err := sd.DACL()
			if err != nil {
				t.Fatal(err)
			}
			if err := windows.SetNamedSecurityInfo(made, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
				t.Fatalf("list the folder swapped in for Everyone: %v", err)
			}
		}, false)
		if err == nil {
			err = r.Close()
			if swap {
				t.Errorf("a folder listed for Everyone swapped in: answered as the folder made, and want ErrOutside")
			}
		}
		if !swap {
			if err != nil {
				t.Fatalf("the control, Mkdir: %v, and want it made", err)
			}
			ownerOnlyFolder(t, made)
			continue
		}
		if !done {
			t.Fatal("Mkdir never reached the step after a folder is made")
		}
		if !errors.Is(err, ErrOutside) {
			t.Errorf("a folder listed for Everyone swapped in: %v, and want ErrOutside", err)
		}
		sd, err := windows.GetNamedSecurityInfo(made, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			t.Fatalf("the folder swapped in is not left at the name: %v", err)
		}
		if !strings.Contains(sd.String(), everyone) {
			t.Errorf("the folder swapped in has the list %s, and want it left with %s", sd, everyone)
		}
	}
}

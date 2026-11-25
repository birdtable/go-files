//go:build windows

package trust

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// What it costs when this fails: on Windows a folder or file held open is judged by the mode Go
// makes up for it, 0777 for a folder, so a sandbox base folder is refused whatever its list says;
// or one whose list lets Everyone write it passes, or still fails once that entry is gone.
func TestHeldOnWindowsJudgesTheListOfTheFolderAndFileItHolds(t *testing.T) {
	t.Parallel()
	d := nativeDir(t)
	p := filepath.Join(d, "f")
	newFile(t, p)
	// The folder's grant to this account passes to what it holds, as lockedDir's does: a list
	// set on the folder spreads to the file, and with a grant on the folder alone the file would
	// hold none, so an account that is not elevated could not open it again.
	mine := entry{mode: windows.GRANT_ACCESS, sid: me(t), mask: full, inherit: windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT}
	for _, c := range []struct {
		name, path string
		k          Kind
		own        entry
	}{{"the folder", d, Folder, mine}, {"the file", p, File, grant(me(t), full)}} {
		f, err := os.Open(c.path)
		if err != nil {
			t.Fatal(err)
		}
		if err := Held(f, c.k, Who{}); err != nil {
			t.Errorf("%s, its list this account's, SYSTEM's and Administrators': %v", c.name, err)
		}
		setList(t, c.path, c.own, grant(everyone, windows.FILE_GENERIC_WRITE))
		wantRefused(t, c.name+" Everyone may write", Held(f, c.k, Who{}), refusal{want: ErrOthersWrite, says: everyone})
		setList(t, c.path, c.own)
		if err := Held(f, c.k, Who{}); err != nil {
			t.Errorf("%s, its grant to Everyone removed: %v", c.name, err)
		}
		wantRefused(t, c.name+" as root's alone", Held(f, c.k, Who{RootOnly: true}), refusal{want: ErrNotOwners, says: "Path gives the name's"})
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		if err := Held(f, c.k, Who{}); err == nil {
			t.Errorf("%s closed: a pass, and want an error", c.name)
		}
	}
}

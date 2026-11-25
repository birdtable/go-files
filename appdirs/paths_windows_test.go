//go:build windows

package appdirs_test

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/birdtable/go-files/appdirs"
)

// On Windows the folders For gives are under the account's real local application data folder,
// as the system names it (FOLDERID_LocalAppData): handed it as LOCALAPPDATA, and, with no
// LOCALAPPDATA, found from the account's real profile folder. testkit points USERPROFILE and
// LOCALAPPDATA at folders of the run's own, and the system expands %USERPROFILE% in the folder it
// names, so the test sets USERPROFILE to the profile folder the account's token gives before it
// asks, so the folder the system names is under the account's real profile, not the run's.
//
// What it costs when this fails: a program on Windows keeps its files in a folder the system does
// not know as the account's, one the roaming profile copies, or none at all.
func TestOnWindowsTheFoldersAreUnderTheRealLocalAppData(t *testing.T) {
	profile, err := windows.GetCurrentProcessToken().GetUserProfileDirectory()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("USERPROFILE", profile) // serial: the process environment, which KnownFolderPath expands
	local, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(local); err != nil || !info.IsDir() {
		t.Fatalf("the local application data folder %q is %v, %v, want a folder", local, info, err)
	}
	given, err := appdirs.For("windows", "app", profile, vars(map[string]string{"LOCALAPPDATA": local}), appdirs.XDG)
	if err != nil {
		t.Fatal(err)
	}
	found, err := appdirs.For("windows", "app", profile, nil, appdirs.XDG)
	if err != nil {
		t.Fatal(err)
	}
	want := windowsUnder(strings.TrimRight(local, `\`))
	if given != want {
		t.Errorf("with LOCALAPPDATA %q:\n got %+v\nwant %+v", local, given, want)
	}
	if !strings.EqualFold(found.Config, want.Config) || !strings.EqualFold(found.Cache, want.Cache) {
		t.Errorf("from the profile folder %q:\n got %+v\nwant %+v", profile, found, want)
	}
	t.Logf("local application data %q, profile %q, Config %q", local, profile, given.Config)
}

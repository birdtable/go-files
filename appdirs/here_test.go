package appdirs_test

import (
	"errors"
	"testing"

	"github.com/birdtable/go-files/appdirs"
)

// Where the system will not say this account's home, Here gives ErrHome and no folder, after the
// layout and the name are checked, as For checks them.
//
// What it costs when this fails: a program started with HOME unset keeps its files under the
// folder it was started in, a checkout among them.
func TestHereWithNoHomeIsErrHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	got, err := appdirs.Here("app", appdirs.XDG)
	if !errors.Is(err, appdirs.ErrHome) || got != (appdirs.Folders{}) {
		t.Errorf("Here with no home: %+v, %v, want ErrHome and no folder", got, err)
	}
	if _, err := appdirs.Here("..", appdirs.XDG); !errors.Is(err, appdirs.ErrApp) || errors.Is(err, appdirs.ErrHome) {
		t.Errorf("Here with no home and a bad name: %v, want ErrApp first", err)
	}
	if _, err := appdirs.Here("app", 0); !errors.Is(err, appdirs.ErrLayout) || errors.Is(err, appdirs.ErrHome) {
		t.Errorf("Here with no home and no layout: %v, want ErrLayout first", err)
	}
	t.Setenv("HOME", "relative/home")
	t.Setenv("USERPROFILE", `relative\home`)
	if got, err := appdirs.Here("app", appdirs.XDG); !errors.Is(err, appdirs.ErrHome) || got != (appdirs.Folders{}) {
		t.Errorf("Here with a relative home: %+v, %v, want ErrHome and no folder", got, err)
	}
}

// System gives each system's machine-wide folder: %ProgramData% on Windows, or C:\ProgramData
// where it is not an absolute path on a drive, /Library/Application Support on macOS, and
// /etc/xdg on the Unix systems, whatever XDG_CONFIG_DIRS says.
//
// What it costs when this fails: a program reads the settings every account shares from a folder
// an account's own variable chose, which that account can write, or from one an administrator
// does not look in; or from a share a variable names, whose host the open offers this account's
// credentials.
func TestSystemGivesEachSystemsFolder(t *testing.T) {
	t.Parallel()
	steer := map[string]string{"XDG_CONFIG_DIRS": "/home/u/mine", "XDG_CONFIG_HOME": "/home/u/mine", "HOME": "/home/u"}
	cases := []struct {
		what, goos string
		env        map[string]string
		want       string
	}{
		{"linux", "linux", nil, "/etc/xdg/app"},
		{"linux, XDG_CONFIG_DIRS, which it does not read", "linux", steer, "/etc/xdg/app"},
		{"freebsd, XDG_CONFIG_DIRS, which it does not read", "freebsd", steer, "/etc/xdg/app"},
		{"openbsd", "openbsd", nil, "/etc/xdg/app"},
		{"darwin", "darwin", nil, "/Library/Application Support/app"},
		{"darwin, XDG_CONFIG_DIRS, which it does not read", "darwin", steer, "/Library/Application Support/app"},
		{"windows, no variable", "windows", nil, `C:\ProgramData\app`},
		{"windows, PROGRAMDATA", "windows", map[string]string{"PROGRAMDATA": `D:\Data`}, `D:\Data\app`},
		{"windows, PROGRAMDATA with a trailing separator", "windows", map[string]string{"PROGRAMDATA": `D:\Data\`}, `D:\Data\app`},
		{"windows, PROGRAMDATA relative", "windows", map[string]string{"PROGRAMDATA": `Data`}, `C:\ProgramData\app`},
		{"windows, PROGRAMDATA a device path", "windows", map[string]string{"PROGRAMDATA": `\\?\D:\Data`}, `C:\ProgramData\app`},
		{"windows, PROGRAMDATA climbing out", "windows", map[string]string{"PROGRAMDATA": `C:\Users\u\..\..\Temp`}, `C:\ProgramData\app`},
		{"windows, PROGRAMDATA a share", "windows", map[string]string{"PROGRAMDATA": `\\host\share`}, `C:\ProgramData\app`},
		{"windows, PROGRAMDATA a share in slashes", "windows", map[string]string{"PROGRAMDATA": `//host/share/x`}, `C:\ProgramData\app`},
	}
	for _, c := range cases {
		got, err := appdirs.System(c.goos, "app", vars(c.env))
		if err != nil || got != c.want {
			t.Errorf("%s: %q, %v, want %q", c.what, got, err, c.want)
		}
	}
	if got, err := appdirs.System("windows", "app", nil); err != nil || got != `C:\ProgramData\app` {
		t.Errorf("windows, a nil env: %q, %v", got, err)
	}
}

// System refuses a name For refuses, with ErrApp, and a system For names no folders for, with
// ErrSystem, and gives no folder.
//
// What it costs when this fails: a name from a flag climbs out of /etc/xdg, or names a device
// Windows opens in a folder's place; a program on plan9 is handed a Unix folder that means nothing
// there.
func TestSystemRefusesWhatForRefuses(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"linux", "darwin", "windows"} {
		for _, app := range []string{"", "..", "../etc", "a/b", `a\b`, "app.", "CON", "a:b"} {
			if got, err := appdirs.System(s, app, nil); !errors.Is(err, appdirs.ErrApp) || got != "" {
				t.Errorf("%s, app %q: %q, %v, want ErrApp and no folder", s, app, got, err)
			}
		}
	}
	for _, g := range []string{"", "plan9", "js", "Linux", "macos"} {
		if got, err := appdirs.System(g, "app", nil); !errors.Is(err, appdirs.ErrSystem) || got != "" {
			t.Errorf("system %q: %q, %v, want ErrSystem and no folder", g, got, err)
		}
	}
}

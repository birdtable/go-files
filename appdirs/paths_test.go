package appdirs_test

import (
	"errors"
	"testing"

	"github.com/birdtable/go-files/appdirs"
)

// vars is an environment of the variables named, and nothing else.
func vars(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

// The homes the table gives For, one per kind of system.
const (
	unixHome = "/home/u"
	winHome  = `C:\Users\u`
)

// every is each XDG variable set to an absolute folder of its own.
func every() map[string]string {
	return map[string]string{
		"XDG_CONFIG_HOME": "/x/config",
		"XDG_STATE_HOME":  "/x/state",
		"XDG_CACHE_HOME":  "/x/cache",
		"XDG_RUNTIME_DIR": "/run/user/1000",
		"XDG_DATA_HOME":   "/x/data",
	}
}

// defaults is the XDG layout under home, with no variable set, so no Run.
func defaults() appdirs.Folders {
	return appdirs.Folders{
		Config: "/home/u/.config/app",
		State:  "/home/u/.local/state/app",
		Cache:  "/home/u/.cache/app",
		Data:   "/home/u/.local/share/app",
	}
}

// runErr is whether err is what For gives with want: ErrRun where want has no Run, and nil where
// it has one.
func runErr(err error, want appdirs.Folders) bool {
	if want.Run == "" {
		return errors.Is(err, appdirs.ErrRun)
	}
	return err == nil
}

// fromEvery is the XDG layout with every variable of every set.
func fromEvery() appdirs.Folders {
	return appdirs.Folders{
		Config: "/x/config/app",
		State:  "/x/state/app",
		Cache:  "/x/cache/app",
		Run:    "/run/user/1000/app",
		Data:   "/x/data/app",
	}
}

// nativeLayout is macOS's own layout under unixHome.
func nativeLayout() appdirs.Folders {
	return appdirs.Folders{
		Config: "/home/u/Library/Application Support/app",
		State:  "/home/u/Library/Application Support/app",
		Cache:  "/home/u/Library/Caches/app",
		Run:    "/home/u/Library/Application Support/app",
		Data:   "/home/u/Library/Application Support/app",
	}
}

// windowsUnder is the Windows layout with base as the local application data folder.
func windowsUnder(base string) appdirs.Folders {
	return appdirs.Folders{
		Config: base + `\app`,
		State:  base + `\app\State`,
		Cache:  base + `\app\Cache`,
		Run:    base + `\app\Run`,
		Data:   base + `\app`,
	}
}

// For gives each system's folders: the XDG variables or their defaults on Linux, FreeBSD and
// macOS in XDG, ~/Library on macOS in Native, and the local application data folder on Windows,
// each joined with the program's name. A value that is not an absolute path is read as unset. In
// the XDG layout with no XDG_RUNTIME_DIR, Run is "" and the error matches ErrRun.
//
// What it costs when this fails: a program keeps its settings, logs or cache in a folder the
// person does not look in, or in Windows's roaming folder, which follows the account to every
// machine; a relative value in a checkout's environment puts a folder under the working folder;
// or a program's sockets go in its State folder, which outlives the session, with no error.
func TestForGivesEachSystemsFolders(t *testing.T) {
	t.Parallel()
	relative := map[string]string{
		"XDG_CONFIG_HOME": "config", "XDG_STATE_HOME": "./state", "XDG_CACHE_HOME": "../cache",
		"XDG_RUNTIME_DIR": "run/user", "XDG_DATA_HOME": "~/data", "LOCALAPPDATA": `AppData\Local`,
	}
	cases := []struct {
		what, goos, home string
		env              map[string]string
		layout           appdirs.Layout
		want             appdirs.Folders
	}{
		{"linux, no variable", "linux", unixHome, nil, appdirs.XDG, defaults()},
		{"linux, every variable", "linux", unixHome, every(), appdirs.XDG, fromEvery()},
		{"linux, every variable relative", "linux", unixHome, relative, appdirs.XDG, defaults()},
		{"linux, every variable empty", "linux", unixHome, map[string]string{"XDG_CONFIG_HOME": "", "XDG_RUNTIME_DIR": ""}, appdirs.XDG, defaults()},
		{"linux, every variable but XDG_RUNTIME_DIR, so no Run and never State", "linux", unixHome,
			map[string]string{"XDG_CONFIG_HOME": "/x/config", "XDG_STATE_HOME": "/x/state", "XDG_CACHE_HOME": "/x/cache", "XDG_DATA_HOME": "/x/data"}, appdirs.XDG,
			appdirs.Folders{Config: "/x/config/app", State: "/x/state/app", Cache: "/x/cache/app", Data: "/x/data/app"}},
		{"linux, Native changes nothing", "linux", unixHome, every(), appdirs.Native, fromEvery()},
		{"linux, Native with no variable, so no Run", "linux", unixHome, nil, appdirs.Native, defaults()},
		{"linux, a home with a trailing slash", "linux", unixHome + "/", nil, appdirs.XDG, defaults()},
		{"linux, XDG_CACHE_HOME the same as XDG_CONFIG_HOME", "linux", unixHome, map[string]string{"XDG_CACHE_HOME": "/x", "XDG_CONFIG_HOME": "/x"}, appdirs.XDG,
			appdirs.Folders{Config: "/x/app", State: defaults().State, Cache: "/x/app/Cache", Run: defaults().Run, Data: defaults().Data}},
		{"linux, Cache holding Config", "linux", unixHome, map[string]string{"XDG_CACHE_HOME": "/x", "XDG_CONFIG_HOME": "/x/app"}, appdirs.XDG,
			appdirs.Folders{Config: "/x/app/app", State: defaults().State, Cache: "/x/app/Cache", Run: defaults().Run, Data: defaults().Data}},
		{"linux, XDG_CACHE_HOME the same as XDG_DATA_HOME", "linux", unixHome, map[string]string{"XDG_CACHE_HOME": "/x", "XDG_DATA_HOME": "/x"}, appdirs.XDG,
			appdirs.Folders{Config: defaults().Config, State: defaults().State, Cache: "/x/app/Cache", Run: defaults().Run, Data: "/x/app"}},
		{"linux, Cache holding Data two down", "linux", unixHome, map[string]string{"XDG_CACHE_HOME": "/x", "XDG_DATA_HOME": "/x/app/Cache"}, appdirs.XDG,
			appdirs.Folders{Config: defaults().Config, State: defaults().State, Cache: "/x/app/Cache/Cache", Run: defaults().Run, Data: "/x/app/Cache/app"}},
		{"freebsd, no variable", "freebsd", unixHome, nil, appdirs.XDG, defaults()},
		{"freebsd, every variable", "freebsd", unixHome, every(), appdirs.XDG, fromEvery()},
		{"freebsd, every variable relative", "freebsd", unixHome, relative, appdirs.XDG, defaults()},
		{"darwin XDG, no variable, so no Run", "darwin", unixHome, nil, appdirs.XDG, defaults()},
		{"darwin XDG, every variable", "darwin", unixHome, every(), appdirs.XDG, fromEvery()},
		{"darwin XDG, every variable relative", "darwin", unixHome, relative, appdirs.XDG, defaults()},
		{"darwin Native, no variable", "darwin", unixHome, nil, appdirs.Native, nativeLayout()},
		{"darwin Native, every variable, which it does not read", "darwin", unixHome, every(), appdirs.Native, nativeLayout()},
		{"windows, no variable", "windows", winHome, nil, appdirs.XDG, windowsUnder(`C:\Users\u\AppData\Local`)},
		{"windows, LOCALAPPDATA", "windows", winHome, map[string]string{"LOCALAPPDATA": `D:\Local`}, appdirs.XDG, windowsUnder(`D:\Local`)},
		{"windows, LOCALAPPDATA with a trailing separator", "windows", winHome, map[string]string{"LOCALAPPDATA": `D:\Local\`}, appdirs.XDG, windowsUnder(`D:\Local`)},
		{"windows, LOCALAPPDATA relative", "windows", winHome, relative, appdirs.XDG, windowsUnder(`C:\Users\u\AppData\Local`)},
		{"windows, LOCALAPPDATA a device path", "windows", winHome, map[string]string{"LOCALAPPDATA": `\\?\D:\Local`}, appdirs.XDG, windowsUnder(`C:\Users\u\AppData\Local`)},
		{"windows, the roaming APPDATA, which it does not read", "windows", winHome, map[string]string{"APPDATA": `D:\Roaming`}, appdirs.XDG, windowsUnder(`C:\Users\u\AppData\Local`)},
		{"windows, the XDG variables, which it does not read", "windows", winHome, every(), appdirs.XDG, windowsUnder(`C:\Users\u\AppData\Local`)},
		{"windows, Native changes nothing", "windows", winHome, nil, appdirs.Native, windowsUnder(`C:\Users\u\AppData\Local`)},
		{"windows, a home with forward slashes", "windows", "C:/Users/u", nil, appdirs.XDG, windowsUnder(`C:\Users\u\AppData\Local`)},
		{"windows, a home on a share", "windows", `\\server\homes\u`, nil, appdirs.XDG, windowsUnder(`\\server\homes\u\AppData\Local`)},
	}
	ran := 0
	for _, c := range cases {
		got, err := appdirs.For(c.goos, "app", c.home, vars(c.env), c.layout)
		if !runErr(err, c.want) || got != c.want {
			t.Errorf("%s:\n got %+v, %v\nwant %+v", c.what, got, err, c.want)
		}
		ran++
	}
	if ran != len(cases) {
		t.Fatalf("ran %d of %d cases", ran, len(cases))
	}
}

// Each XDG variable, set alone, moves its own folder and no other; Run, with no XDG_RUNTIME_DIR,
// is "" with ErrRun, and never goes with State. A nil environment is one with no variable set.
//
// What it costs when this fails: a person who moves one folder, the cache to a larger disk, finds
// the program's settings or state moved with it, or left behind when they meant to move them.
func TestEachVariableMovesItsOwnFolderAlone(t *testing.T) {
	t.Parallel()
	moved := map[string]func(f *appdirs.Folders, to string){
		"XDG_CONFIG_HOME": func(f *appdirs.Folders, to string) { f.Config = to },
		"XDG_STATE_HOME":  func(f *appdirs.Folders, to string) { f.State = to },
		"XDG_CACHE_HOME":  func(f *appdirs.Folders, to string) { f.Cache = to },
		"XDG_RUNTIME_DIR": func(f *appdirs.Folders, to string) { f.Run = to },
		"XDG_DATA_HOME":   func(f *appdirs.Folders, to string) { f.Data = to },
	}
	ran := 0
	for _, goos := range []string{"linux", "darwin", "freebsd"} {
		if got, err := appdirs.For(goos, "app", unixHome, nil, appdirs.XDG); !runErr(err, defaults()) || got != defaults() {
			t.Errorf("%s, a nil environment: %+v, %v, want %+v and ErrRun", goos, got, err, defaults())
		}
		for key, move := range moved {
			want := defaults()
			move(&want, "/v/app")
			got, err := appdirs.For(goos, "app", unixHome, vars(map[string]string{key: "/v"}), appdirs.XDG)
			if !runErr(err, want) || got != want {
				t.Errorf("%s, %s alone:\n got %+v, %v\nwant %+v", goos, key, got, err, want)
			}
			ran++
		}
	}
	if ran != 3*len(moved) {
		t.Fatalf("ran %d of %d cases", ran, 3*len(moved))
	}
}

// system is one of For's systems, with the layout it is given.
type system struct {
	name, goos string
	layout     appdirs.Layout
}

// systems are For's four systems, with macOS in each layout, for the refusals.
func systems() []system {
	return []system{
		{"linux", "linux", appdirs.XDG},
		{"freebsd", "freebsd", appdirs.XDG},
		{"darwin XDG", "darwin", appdirs.XDG},
		{"darwin Native", "darwin", appdirs.Native},
		{"windows", "windows", appdirs.XDG},
	}
}

// A layout neither XDG nor Native, the zero Layout of a call that chose none among them, is
// refused with ErrLayout on every system, and For gives no folder, whatever else it is handed.
//
// What it costs when this fails: a program that never chose a layout is handed one on macOS, and
// its files land in the layout its author did not mean, found by a person on a Mac after release.
func TestALayoutNeitherXDGNorNativeIsRefusedWithErrLayout(t *testing.T) {
	t.Parallel()
	layouts := []appdirs.Layout{0, appdirs.Native + 1, -1}
	ran := 0
	for _, s := range systems() {
		for _, l := range layouts {
			got, err := appdirs.For(s.goos, "app", homeOf(s.goos), vars(every()), l)
			if !errors.Is(err, appdirs.ErrLayout) || errors.Is(err, appdirs.ErrRun) || got != (appdirs.Folders{}) {
				t.Errorf("%s, layout %d: %+v, %v, want ErrLayout and no folder", s.name, l, got, err)
			}
			ran++
		}
	}
	if got, err := appdirs.For("darwin", "..", "", nil, 0); !errors.Is(err, appdirs.ErrLayout) || got != (appdirs.Folders{}) {
		t.Errorf("darwin, no layout, a bad name and home: %+v, %v, want ErrLayout first", got, err)
	}
	if ran != len(systems())*len(layouts) {
		t.Fatalf("ran %d of %d cases", ran, len(systems())*len(layouts))
	}
}

// homeOf is a home that is good for goos.
func homeOf(goos string) string {
	if goos == "windows" {
		return winHome
	}
	return unixHome
}

// A home that is empty, relative, or absolute only for another system is refused with ErrHome,
// on each system, and For gives no folder.
//
// What it costs when this fails: a program started with HOME unset or relative keeps its files
// under whatever folder it was started in, a checkout among them, and reads settings a repository
// planted there.
func TestABadHomeIsRefusedWithErrHome(t *testing.T) {
	t.Parallel()
	unix := []string{"", "home/u", "./u", "~", "~/u", `C:\Users\u`, `\\server\homes\u`}
	windows := []string{
		"", `Users\u`, `.\u`, "~", "/home/u", `\Users\u`, `C:Users\u`, "C:", `\\?\C:\Users\u`, `\\.\C:\Users\u`,
		`\\server`, `\\server\`, `\\\homes\u`, `C:\Users\..\..\Windows`, `C:\Users\.\u`, `1:\Users\u`,
	}
	ran := 0
	for _, s := range systems() {
		homes := unix
		if s.goos == "windows" {
			homes = windows
		}
		for _, home := range homes {
			got, err := appdirs.For(s.goos, "app", home, nil, s.layout)
			if !errors.Is(err, appdirs.ErrHome) || errors.Is(err, appdirs.ErrApp) || got != (appdirs.Folders{}) {
				t.Errorf("%s, home %q: %+v, %v, want ErrHome and no folder", s.name, home, got, err)
			}
			ran++
		}
	}
	if want := 4*len(unix) + len(windows); ran != want {
		t.Fatalf("ran %d of %d cases", ran, want)
	}
}

// A program's name that is not one plain folder name on every system is refused with ErrApp, on
// each system, and For gives no folder.
//
// What it costs when this fails: a name taken from a flag or a file climbs out of the folder it
// was to name (".."), names a folder two deep, one Windows reads as another ("app." is "app"), or
// a device Windows opens in its place ("CON").
func TestABadAppIsRefusedWithErrApp(t *testing.T) {
	t.Parallel()
	apps := []string{
		"", ".", "..", "../app", "app/..", "a..b", "a/b", "/app", `a\b`, `\app`, "a:b", "C:", "a\x00b", "a\nb", "a\tb", "a\x7fb",
		"app.", "app ", "app...", "a<b", "a>b", `a"b`, "a|b", "a?b", "a*b",
		"CON", "nul", "Aux", "prn.txt", "COM1", "lpt9.log", "con .d", "COM¹", "CONIN$", "conout$",
	}
	ran := 0
	for _, s := range systems() {
		for _, app := range apps {
			got, err := appdirs.For(s.goos, app, homeOf(s.goos), nil, s.layout)
			if !errors.Is(err, appdirs.ErrApp) || errors.Is(err, appdirs.ErrHome) || got != (appdirs.Folders{}) {
				t.Errorf("%s, app %q: %+v, %v, want ErrApp and no folder", s.name, app, got, err)
			}
			ran++
		}
	}
	if ran != len(systems())*len(apps) {
		t.Fatalf("ran %d of %d cases", ran, len(systems())*len(apps))
	}
	for _, app := range []string{"app", "go-files", "My App", ".app", "app.d", "a.b.c", "émigré", "_", "CONSOLE", "com10", "COM", "nullify", "LPTX"} {
		if _, err := appdirs.For("windows", app, winHome, nil, appdirs.XDG); err != nil {
			t.Errorf("app %q: %v, want it taken", app, err)
		}
	}
}

// A system For names no folders for is refused with ErrSystem, and For gives no folder.
//
// What it costs when this fails: a program on plan9 or in a browser is handed Unix folders that
// mean nothing there, or a mistyped "Linux" is read as some system's layout.
func TestAnUnknownSystemIsRefusedWithErrSystem(t *testing.T) {
	t.Parallel()
	ran := 0
	goos := []string{"", "plan9", "js", "wasip1", "Linux", "Windows", "macos"}
	for _, g := range goos {
		got, err := appdirs.For(g, "app", unixHome, nil, appdirs.XDG)
		if !errors.Is(err, appdirs.ErrSystem) || got != (appdirs.Folders{}) {
			t.Errorf("system %q: %+v, %v, want ErrSystem and no folder", g, got, err)
		}
		ran++
	}
	if ran != len(goos) {
		t.Fatalf("ran %d of %d cases", ran, len(goos))
	}
}

// For reads no environment of its own. TestMain points HOME and every XDG variable at the tests'
// folders, and For, given a nil env, gives the defaults under the home it was handed all the same.
//
// What it costs when this fails: a test of a program's Windows folders run on Linux, or a program
// that reads its environment once and hands it on, finds For reading the real one behind its back.
func TestForReadsNoEnvironmentOfItsOwn(t *testing.T) {
	t.Parallel()
	got, err := appdirs.For("linux", "app", unixHome, nil, appdirs.XDG)
	if !runErr(err, defaults()) || got != defaults() {
		t.Errorf("For with a nil env: %+v, %v, want %+v and ErrRun", got, err, defaults())
	}
}

// For reads each variable once, and the value it checks is the value it uses. A reader that
// answers an absolute folder and then a relative one gives the absolute folder, from one read.
//
// What it costs when this fails: an environment that changes between two reads, or a reader with
// a cost, hands For a relative folder it checked as absolute, and the program writes beneath its
// working folder.
func TestForReadsEachVariableOnceAndUsesTheValueItChecked(t *testing.T) {
	t.Parallel()
	reads := map[string]int{}
	env := func(k string) string {
		reads[k]++
		if reads[k]%2 == 1 {
			return "/abs/" + k
		}
		return "relative/" + k
	}
	got, err := appdirs.For("linux", "app", unixHome, env, appdirs.XDG)
	want := appdirs.Folders{
		Config: "/abs/XDG_CONFIG_HOME/app",
		State:  "/abs/XDG_STATE_HOME/app",
		Cache:  "/abs/XDG_CACHE_HOME/app",
		Run:    "/abs/XDG_RUNTIME_DIR/app",
		Data:   "/abs/XDG_DATA_HOME/app",
	}
	if err != nil || got != want {
		t.Errorf("For with a reader that changes its answer: %+v, %v, want %+v", got, err, want)
	}
	for k, n := range reads {
		if n != 1 {
			t.Errorf("%s read %d times, want once", k, n)
		}
	}
}

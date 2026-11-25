package appdirs

import (
	"errors"
	"maps"
	"math/rand"
	"testing"
)

// envOf reads m as an environment: a variable m does not hold is unset.
func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// HereWith reads the home from the variable its system's here_*.go file names, and every other
// variable from the env handed in: each row sets both HOME and USERPROFILE, to homes apart, so a
// call that read the other one's would answer another home, or ErrHome. Each kind's row runs on
// every system, on hereWith, which HereWith calls with the kind and variable it is built with.
//
// What it costs when this fails: a program given an environment of its own, as each of a parallel
// test's programs is, keeps its files under another home, or in the folders its own XDG variables
// do not name, or in the XDG layout on macOS where it asked for Native.
func TestHereWithReadsTheHomeAndEveryVariableFromEnv(t *testing.T) {
	t.Parallel()
	cases := []struct {
		what   string
		k      kind
		key    string
		layout Layout
		env    map[string]string
		want   Folders
	}{
		{
			"windows", windowsKind, "USERPROFILE", XDG,
			map[string]string{"USERPROFILE": `C:\Users\u`, "HOME": "/home/not"},
			Folders{Config: `C:\Users\u\AppData\Local\app`, State: `C:\Users\u\AppData\Local\app\State`, Cache: `C:\Users\u\AppData\Local\app\Cache`, Run: `C:\Users\u\AppData\Local\app\Run`, Data: `C:\Users\u\AppData\Local\app`},
		},
		{
			"darwin, Native", darwinKind, "HOME", Native,
			map[string]string{"HOME": "/Users/u", "USERPROFILE": `C:\Users\not`, "XDG_CONFIG_HOME": "/cfg", "XDG_RUNTIME_DIR": "/run/u"},
			Folders{Config: "/Users/u/Library/Application Support/app", State: "/Users/u/Library/Application Support/app", Cache: "/Users/u/Library/Caches/app", Run: "/Users/u/Library/Application Support/app", Data: "/Users/u/Library/Application Support/app"},
		},
		{
			"xdg", xdgKind, "HOME", XDG,
			map[string]string{"HOME": "/home/u", "USERPROFILE": `C:\Users\not`, "XDG_CONFIG_HOME": "/cfg", "XDG_RUNTIME_DIR": "/run/user/1"},
			Folders{Config: "/cfg/app", State: "/home/u/.local/state/app", Cache: "/home/u/.cache/app", Run: "/run/user/1/app", Data: "/home/u/.local/share/app"},
		},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			t.Parallel()
			got, err := hereWith(c.k, c.key, "app", c.layout, envOf(c.env))
			if err != nil || got != c.want {
				t.Errorf("%s: %+v, %v, want %+v", c.what, got, err, c.want)
			}
		})
	}
}

// A nil env, a home that is empty or unset, or one For refuses, is ErrHome and no folder, on each
// kind, where every other variable names a folder: HereWith never reads the process's own home,
// which the test's process has. The layout and the name are refused first, as For refuses them.
//
// What it costs when this fails: a program a test gave no home keeps its files in the home of the
// account that runs the tests, beside every other test's, or under the folder it was started in.
func TestHereWithNoHomeInEnvIsErrHome(t *testing.T) {
	t.Parallel()
	others := map[string]string{"LOCALAPPDATA": `D:\Local`, "XDG_CONFIG_HOME": "/cfg", "XDG_STATE_HOME": "/st", "XDG_CACHE_HOME": "/ca", "XDG_DATA_HOME": "/da", "XDG_RUNTIME_DIR": "/run/u"}
	with := func(k, v string) map[string]string {
		m := map[string]string{k: v}
		maps.Copy(m, others)
		return m
	}
	for _, s := range []struct {
		what      string
		k         kind
		key       string
		other     string
		relative  string
		elsewhere string
	}{
		{"windows", windowsKind, "USERPROFILE", "HOME", `Users\u`, `C:\Users\u`},
		{"darwin", darwinKind, "HOME", "USERPROFILE", "Users/u", "/Users/u"},
		{"xdg", xdgKind, "HOME", "USERPROFILE", "home/u", "/home/u"},
	} {
		for _, layout := range []Layout{XDG, Native} {
			for what, env := range map[string]func(string) string{
				"a nil env":                    nil,
				"the home unset":               envOf(others),
				"the home empty":               envOf(with(s.key, "")),
				"the home relative":            envOf(with(s.key, s.relative)),
				"the home in the other's name": envOf(with(s.other, s.elsewhere)),
			} {
				if got, err := hereWith(s.k, s.key, "app", layout, env); !errors.Is(err, ErrHome) || got != (Folders{}) {
					t.Errorf("%s, layout %d, %s: %+v, %v, want ErrHome and no folder", s.what, layout, what, got, err)
				}
			}
		}
		if _, err := hereWith(s.k, s.key, "..", XDG, nil); !errors.Is(err, ErrApp) || errors.Is(err, ErrHome) {
			t.Errorf("%s, a nil env and a bad name: %v, want ErrApp first", s.what, err)
		}
		if _, err := hereWith(s.k, s.key, "app", 0, nil); !errors.Is(err, ErrLayout) || errors.Is(err, ErrHome) {
			t.Errorf("%s, a nil env and no layout: %v, want ErrLayout first", s.what, err)
		}
	}
	if got, err := HereWith("app", XDG, nil); !errors.Is(err, ErrHome) || got != (Folders{}) {
		t.Errorf("HereWith on this system, a nil env: %+v, %v, want ErrHome and no folder", got, err)
	}
}

// For any name, layout and environment, a nil one among them, HereWith answers exactly as For does
// on the system it is built for, handed the home the variable os.UserHomeDir reads there: the same
// folders, and the same error, or none. The system and its home's variable are the test's own,
// written here and not read from the package. 20,000 draws, from a fixed seed.
//
// What it costs when this fails: HereWith and For drift apart, so a program that moves from one to
// the other, or a test that checks For for the folders HereWith gives, keeps its files in folders
// the other does not name, or misses a refusal one of them makes.
func TestHereWithIsForOnTheSystemItIsBuiltFor(t *testing.T) {
	t.Parallel()
	sys, ok := map[kind]struct{ goos, key string }{
		windowsKind: {"windows", "USERPROFILE"},
		darwinKind:  {"darwin", "HOME"},
		xdgKind:     {"linux", "HOME"},
	}[thisKind]
	if !ok {
		t.Fatalf("appdirs is built for kind %d, which For names no folders for, and the tests run on none", thisKind)
	}
	apps := []string{"app", "My App", "a.b", "é", "", "..", "a/b", `a\b`, "CON", "app."}
	layouts := []Layout{XDG, Native, 0, 3}
	keys := []string{"HOME", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR"}
	values := []string{
		"", "rel", "./rel", "../up", "~/x", "/home/u", "/Users/u/", "/", "//two", "/abs/../up", "/abs/app",
		`C:\Users\u`, `c:/Users/u`, `D:\`, `\\srv\homes\u`, `\\?\D:\x`, `\\.\pipe\x`, `D:\a\..\b`, `D:rel`, `\rooted`,
	}
	r := rand.New(rand.NewSource(11)) //nolint:gosec // G404: a fixed seed, so a failure is the same on every run
	const draws = 20000
	checked := 0
	for range draws {
		app, layout := apps[r.Intn(len(apps))], layouts[r.Intn(len(layouts))]
		var env func(string) string
		m := map[string]string{}
		if r.Intn(8) != 0 {
			for range r.Intn(len(keys) + 1) {
				m[keys[r.Intn(len(keys))]] = values[r.Intn(len(values))]
			}
			env = envOf(m)
		}
		got, err := HereWith(app, layout, env)
		home := ""
		if env != nil {
			home = env(sys.key)
		}
		want, werr := For(sys.goos, app, home, env, layout)
		if got != want || (err == nil) != (werr == nil) || err != nil && err.Error() != werr.Error() {
			t.Fatalf("app %q, layout %d, env %v (nil: %t): HereWith gives %+v, %v, and For on %s %+v, %v", app, layout, m, env == nil, got, err, sys.goos, want, werr)
		}
		for _, e := range []error{ErrHome, ErrApp, ErrLayout, ErrRun, ErrSystem} {
			if errors.Is(err, e) != errors.Is(werr, e) {
				t.Fatalf("app %q, layout %d, env %v: HereWith's %v and For's %v differ on %v", app, layout, m, err, werr, e)
			}
		}
		checked++
	}
	if checked != draws {
		t.Fatalf("checked %d of %d draws", checked, draws)
	}
}

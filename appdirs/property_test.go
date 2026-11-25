package appdirs_test

import (
	"errors"
	"math/rand"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/quick"

	"github.com/birdtable/go-files/appdirs"
)

// draw is one call of For the property test makes: a system, a name, a home, an environment and
// whether the layout is Native or XDG.
type draw struct {
	goos, app, home string
	env             map[string]string
	native          bool
}

// layout is the Layout d is drawn with.
func (d draw) layout() appdirs.Layout {
	if d.native {
		return appdirs.Native
	}
	return appdirs.XDG
}

// pick is one of from, at random.
func pick(r *rand.Rand, from ...string) string { return from[r.Intn(len(from))] }

// values are what a variable may be drawn as: absolute for one system or the other, relative, empty, or
// with a climb in it.
func values() []string {
	return []string{
		"", "rel", "./rel", "../up", "~/x", "/abs", "/abs/../up", "/abs/./x", "/", "//two",
		`D:\Local`, `D:/Local`, `D:\Local\`, `\\srv\share\x`, `\\?\D:\x`, `\\.\pipe\x`, `D:\a\..\b`, `D:rel`, `\rooted`,
	}
}

// keys are the variables a draw may set.
func keys() []string {
	return []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR", "XDG_DATA_HOME", "LOCALAPPDATA", "APPDATA", "HOME"}
}

// generate draws a call of For whose system, name and home are good, and whose environment is
// anything.
func generate(args []reflect.Value, r *rand.Rand) {
	d := draw{goos: pick(r, "linux", "darwin", "windows", "freebsd"), native: r.Intn(2) == 0, env: map[string]string{}}
	// No o, p or u, so no draw is a Windows device (CON, PRN, AUX, NUL, COM1, LPT1), which For refuses.
	letters := []rune("abcdefghijklmnqrstvwxyzABCDEFGHIJKLMNQRSTVWXYZ0123456789-_. é")
	for {
		var b strings.Builder
		for range 1 + r.Intn(12) {
			b.WriteRune(letters[r.Intn(len(letters))])
		}
		d.app = strings.TrimRight(b.String(), ". ")
		if d.app != "" && !strings.Contains(d.app, "..") {
			break
		}
	}
	if d.goos == "windows" {
		d.home = pick(r, `C:\Users\u`, `c:/Users/u`, `D:\`, `\\srv\homes\u`, `C:\Users\u\`)
	} else {
		d.home = pick(r, "/home/u", "/", "/Users/u/", "/var/../home/u", "//home/u")
	}
	for range r.Intn(len(keys()) + 1) {
		d.env[pick(r, keys()...)] = pick(r, values()...)
	}
	// One folder beneath another's: XDG_CACHE_HOME=/abs and XDG_CONFIG_HOME=/abs/<app> make Cache
	// hold Config.
	if r.Intn(3) == 0 {
		d.env[pick(r, keys()...)] = "/abs/" + d.app
	}
	args[0] = reflect.ValueOf(d)
}

// absolute is whether p is an absolute path on goos, read without the package's own check: a
// slash first on Unix, and a drive and a backslash, or two backslashes and a share, on Windows.
func absolute(goos, p string) bool {
	if goos != "windows" {
		return strings.HasPrefix(p, "/")
	}
	if len(p) >= 3 && p[1] == ':' && p[2] == '\\' {
		return true
	}
	host, rest, ok := strings.Cut(strings.TrimPrefix(p, `\\`), `\`)
	share, _, _ := strings.Cut(rest, `\`)
	return strings.HasPrefix(p, `\\`) && ok && host != "" && host != "?" && host != "." && share != ""
}

// elements is p split at goos's separators.
func elements(goos, p string) []string {
	if goos == "windows" {
		return strings.Split(p, `\`)
	}
	return strings.Split(p, "/")
}

// within is whether p is the folder dir or beneath it, on goos, element by element.
func within(goos, dir, p string) bool {
	de, pe := elements(goos, dir), elements(goos, p)
	return len(pe) >= len(de) && slices.Equal(de, pe[:len(de)])
}

// For any good system, name and home, and any environment, every folder For gives is absolute
// for its system, holds the name as an element, and holds no "." or ".." element; and Cache is
// never the folder Config or Data is, nor one that holds either. Run is "", with ErrRun, exactly
// where the XDG layout is taken with no absolute XDG_RUNTIME_DIR. 20,000 draws, from a fixed seed.
//
// What it costs when this fails: a value in the environment puts a folder under the working
// folder, or out of the program's own, or a program that clears its cache deletes its settings,
// or its sockets go in a folder that outlives the session.
func TestEveryFolderIsAbsoluteHoldsTheNameAndCacheIsApart(t *testing.T) {
	t.Parallel()
	checked := 0
	holds := func(d draw) bool {
		f, err := appdirs.For(d.goos, d.app, d.home, vars(d.env), d.layout())
		xdg := d.goos != "windows" && (d.goos != "darwin" || !d.native)
		noRun := xdg && !absolute(d.goos, d.env["XDG_RUNTIME_DIR"])
		if noRun != errors.Is(err, appdirs.ErrRun) || err != nil && !noRun || noRun != (f.Run == "") {
			t.Logf("%+v: %+v, %v", d, f, err)
			return false
		}
		folders := []string{f.Config, f.State, f.Cache, f.Data}
		if !noRun {
			folders = append(folders, f.Run)
		}
		ok := !within(d.goos, f.Cache, f.Config) && !within(d.goos, f.Cache, f.Data)
		for _, p := range folders {
			named := false
			for _, e := range elements(d.goos, p) {
				named = named || e == d.app
				ok = ok && e != "." && e != ".."
			}
			ok = ok && named && absolute(d.goos, p)
		}
		if !ok {
			t.Logf("%+v: %+v", d, f)
		}
		checked++
		return ok
	}
	cfg := &quick.Config{MaxCount: 20000, Rand: rand.New(rand.NewSource(7)), Values: generate} //nolint:gosec // G404: a fixed seed, so a failure is the same on every run
	if err := quick.Check(holds, cfg); err != nil {
		t.Error(err)
	}
	if checked != cfg.MaxCount {
		t.Fatalf("checked %d of %d draws", checked, cfg.MaxCount)
	}
}

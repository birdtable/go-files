package appdirs

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Here is For on the system its build files give, with this account's home and environment: the
// test points the home and every variable For reads at folders of its own, so a Here that read a
// nil environment or another home would differ. Which layout each system builds is the next test's.
//
// What it costs when this fails: a program that calls Here to keep the system and the home out of
// its own code is handed another system's folders, or ones that ignore the person's variables, and
// keeps its settings where the person does not look.
func TestHereIsForOnTheSystemItIsBuiltFor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home) // serial: the process environment, which Here reads
	env := map[string]string{"HOME": home, "USERPROFILE": home}
	for _, k := range []string{"LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR"} {
		env[k] = filepath.Join(home, strings.ToLower(k))
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	for _, layout := range []Layout{XDG, Native} {
		want, werr := folders(thisKind, "app", home, func(k string) string { return env[k] }, layout)
		got, err := Here("app", layout)
		if werr != nil || err != nil || got != want {
			t.Errorf("layout %d in kind %d: Here gives %+v, %v, want For's %+v, %v", layout, thisKind, got, err, want, werr)
		}
	}
}

// Every system Go builds for gives each of windowsPart, darwinPart and xdgPart in exactly one
// here_*.go file, and their sum is the layout For takes for it: noKind where For names no folders.
// Each gives windowsHome, darwinHome and xdgHome once too, and their sum is HomeVar, the variable
// HereWith reads the home from: USERPROFILE on Windows and HOME on macOS and the XDG systems, as
// os.UserHomeDir reads it, and "" where For names no folders. A system's tag implied by another's
// (android's linux, illumos's solaris, ios's darwin) and hurd, which Go counts as Unix, are among
// them.
//
// What it costs when this fails: Here on iOS or hurd gives folders For refuses, a system builds no
// part and appdirs does not compile there, or a part names another layout and every program on that
// system keeps its files where another system keeps them; or HereWith on Windows reads a HOME that
// a Unix tool set, and keeps the program's files where the person does not look, and a test that
// builds its environment from HomeVar gives its program no home there.
func TestEachSystemBuildsTheLayoutForGivesIt(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("here_*.go")
	if err != nil {
		t.Fatal(err)
	}
	value := map[string]kind{"noKind": noKind, "windowsKind": windowsKind, "darwinKind": darwinKind, "xdgKind": xdgKind}
	for _, goos := range []string{"aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos", "ios", "js", "linux", "netbsd", "openbsd", "plan9", "solaris", "wasip1", "windows"} {
		ctx := build.Default
		ctx.GOOS, ctx.GOARCH, ctx.BuildTags = goos, "arm64", nil
		given := map[string][]string{}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			ok, err := ctx.MatchFile(".", f)
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				for name, v := range partsIn(t, f) {
					given[name] = append(given[name], v)
				}
			}
		}
		var sum kind
		for _, part := range []string{"windowsPart", "darwinPart", "xdgPart"} {
			v := given[part]
			k, ok := kind(0), false
			if len(v) == 1 {
				k, ok = value[v[0]]
			}
			if !ok {
				t.Errorf("%s builds %s as %q, want one of noKind, windowsKind, darwinKind, xdgKind, once", goos, part, v)
			}
			sum += k
		}
		if want := kindOf(goos); sum != want {
			t.Errorf("%s builds the parts %v, kind %d, and For takes kind %d for it", goos, given, sum, want)
		}
		home := map[kind]string{windowsKind: "USERPROFILE", darwinKind: "HOME", xdgKind: "HOME"}[kindOf(goos)]
		homes, set := "", 0
		for _, part := range []string{"windowsHome", "darwinHome", "xdgHome"} {
			v := given[part]
			if len(v) != 1 {
				t.Errorf("%s builds %s as %q, want one string, once", goos, part, v)
				continue
			}
			if v[0] != "" {
				set++
			}
			homes += v[0]
		}
		if homes != home || set > 1 {
			t.Errorf("%s builds the home parts %v, HomeVar %q, want %q from at most one part: the variable os.UserHomeDir reads the home from there", goos, given, homes, home)
		}
	}
}

// partsIn is each constant the Go file f declares, by name, with the name of the kind it is the int
// of, or for a home part the string it is.
func partsIn(t *testing.T, f string) map[string]string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for _, d := range file.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, sp := range g.Specs {
			v, ok := sp.(*ast.ValueSpec)
			if !ok || len(v.Names) != 1 || len(v.Values) != 1 {
				t.Fatalf("%s declares a constant not one name to one value", f)
			}
			if s, ok := v.Values[0].(*ast.BasicLit); ok && strings.HasSuffix(v.Names[0].Name, "Home") && s.Kind == token.STRING {
				home, err := strconv.Unquote(s.Value)
				if err != nil {
					t.Fatal(err)
				}
				parts[v.Names[0].Name] = home
				continue
			}
			var fn, id *ast.Ident
			if c, ok := v.Values[0].(*ast.CallExpr); ok && len(c.Args) == 1 {
				fn, _ = c.Fun.(*ast.Ident)
				id, _ = c.Args[0].(*ast.Ident)
			}
			if fn == nil || fn.Name != "int" || id == nil {
				t.Fatalf("%s gives %s no int of a kind's name", f, v.Names[0].Name)
			}
			parts[v.Names[0].Name] = id.Name
		}
	}
	return parts
}

package appdirs

import (
	"errors"
	"maps"
	"strings"
	"testing"
)

// fieldsOf is the five folders of f by name, in Folders' order.
func fieldsOf(f Folders) [5][2]string {
	return [5][2]string{{"Config", f.Config}, {"State", f.State}, {"Cache", f.Cache}, {"Run", f.Run}, {"Data", f.Data}}
}

// sourcesAgree checks src, the Sources of a system and layout, against call, HereWith there: from an
// environment holding only the home, under key, setting any variable to a fresh absolute folder
// changes exactly the fields src names it in, and setting the home to another one changes each field
// src names "". fresh makes an absolute folder on that system. The variables tried are every one any
// system's Sources names, the home's, and ones For reads on no system.
func sourcesAgree(t *testing.T, what string, src Folders, key string, fresh func(string) string, call func(map[string]string) Folders) {
	t.Helper()
	base := map[string]string{key: fresh("home")}
	before := call(base)
	tried := map[string]bool{"HOME": true, "USERPROFILE": true, "APPDATA": true, "PROGRAMDATA": true, "XDG_CONFIG_DIRS": true, "XDG_DATA_DIRS": true, "TMPDIR": true}
	for _, k := range []kind{windowsKind, darwinKind, xdgKind} {
		for _, layout := range []Layout{XDG, Native} {
			s, err := sources(k, layout)
			if err != nil {
				t.Fatalf("sources(%d, %d): %v", k, layout, err)
			}
			for _, f := range fieldsOf(s) {
				if f[1] != "" {
					tried[f[1]] = true
				}
			}
		}
	}
	delete(tried, key)
	for v := range tried {
		env := maps.Clone(base)
		env[v] = fresh(strings.ToLower(v))
		after := call(env)
		for i, f := range fieldsOf(src) {
			was, is := fieldsOf(before)[i][1], fieldsOf(after)[i][1]
			if moved, named := was != is, f[1] == v; moved != named {
				t.Errorf("%s: %s set, %s is %q, was %q (moved: %t), and Sources names %q for it", what, v, f[0], is, was, moved, f[1])
			}
		}
	}
	after := call(map[string]string{key: fresh("other")})
	for i, f := range fieldsOf(src) {
		if was, is := fieldsOf(before)[i][1], fieldsOf(after)[i][1]; f[1] == "" && was == is {
			t.Errorf("%s: another home leaves %s at %q, and Sources names no variable for it, so the home alone moves it", what, f[0], was)
		}
	}
}

// For each kind and layout, Sources names for each folder the variable that moves that folder of
// HereWith's answer, and that one alone: a variable it names moves exactly the folders it names it
// for, a variable it names for none moves none, and a folder it names "" moves with the home. Each
// kind's row runs on every system, on sources and hereWith, which Sources and HereWith call with
// the kind they are built for; the last row is the exported pair on this system.
//
// What it costs when this fails: Sources and For drift apart, and a program that sets the variables
// Sources names, to keep a test's folders apart, still shares a folder with every other test, or
// sets one that moves a folder it meant to leave where it was.
func TestSourcesNamesTheVariableThatMovesEachFolder(t *testing.T) {
	t.Parallel()
	unix := func(s string) string { return "/fresh/" + s }
	for _, s := range []struct {
		what  string
		k     kind
		key   string
		fresh func(string) string
	}{
		{"windows", windowsKind, "USERPROFILE", func(s string) string { return `C:\fresh\` + s }},
		{"darwin", darwinKind, "HOME", unix},
		{"xdg", xdgKind, "HOME", unix},
	} {
		for _, layout := range []Layout{XDG, Native} {
			src, err := sources(s.k, layout)
			if err != nil {
				t.Fatalf("%s, layout %d: %v", s.what, layout, err)
			}
			sourcesAgree(t, s.what, src, s.key, s.fresh, func(m map[string]string) Folders {
				f, err := hereWith(s.k, s.key, "app", layout, envOf(m))
				if err != nil && !errors.Is(err, ErrRun) {
					t.Fatalf("%s, layout %d, env %v: %v", s.what, layout, m, err)
				}
				return f
			})
		}
	}
	fresh := map[kind]func(string) string{windowsKind: func(s string) string { return `C:\fresh\` + s }, darwinKind: unix, xdgKind: unix}[thisKind]
	for _, layout := range []Layout{XDG, Native} {
		src, err := Sources(layout)
		if err != nil {
			t.Fatalf("Sources on this system, layout %d: %v", layout, err)
		}
		sourcesAgree(t, "this system", src, HomeVar, fresh, func(m map[string]string) Folders {
			f, err := HereWith("app", layout, envOf(m))
			if err != nil && !errors.Is(err, ErrRun) {
				t.Fatalf("this system, layout %d, env %v: %v", layout, m, err)
			}
			return f
		})
	}
}

// Sources refuses as For does: a layout neither XDG nor Native is ErrLayout on every kind, first,
// and a system For names no folders for is ErrSystem; each gives no field.
//
// What it costs when this fails: a program that asked with no layout, or on a system For names no
// folders for, sets variables that move nothing, and believes its folders are its own.
func TestSourcesRefusesAsForDoes(t *testing.T) {
	t.Parallel()
	for _, k := range []kind{noKind, windowsKind, darwinKind, xdgKind} {
		for _, layout := range []Layout{0, 3, -1} {
			if got, err := sources(k, layout); !errors.Is(err, ErrLayout) || got != (Folders{}) {
				t.Errorf("kind %d, layout %d: %+v, %v, want ErrLayout and no field", k, layout, got, err)
			}
		}
	}
	for _, layout := range []Layout{XDG, Native} {
		if got, err := sources(noKind, layout); !errors.Is(err, ErrSystem) || got != (Folders{}) {
			t.Errorf("no kind, layout %d: %+v, %v, want ErrSystem and no field", layout, got, err)
		}
	}
	if got, err := Sources(0); !errors.Is(err, ErrLayout) || got != (Folders{}) {
		t.Errorf("Sources on this system, no layout: %+v, %v, want ErrLayout and no field", got, err)
	}
}

// An environment holding HomeVar alone, set to a folder of the test's, gives HereWith a home on the
// system it is built for: no ErrHome, and Config under that folder, in each layout. HomeVar is a
// variable's name there, never "", which a map would hand back as well. The test names no system
// and no variable of its own.
//
// What it costs when this fails: a test that builds its program's environment from HomeVar, as its
// doc says to, gives the program no home on one system, and the program refuses to start there.
func TestHomeVarAloneGivesAHome(t *testing.T) {
	t.Parallel()
	if HomeVar == "" {
		t.Errorf("HomeVar is \"\" on kind %d, which For names folders for", thisKind)
	}
	dir := t.TempDir()
	for _, layout := range []Layout{XDG, Native} {
		f, err := HereWith("app", layout, envOf(map[string]string{HomeVar: dir}))
		if errors.Is(err, ErrHome) || !strings.HasPrefix(f.Config, dir) {
			t.Errorf("layout %d, %s=%q alone: %+v, %v, want no ErrHome and Config under it", layout, HomeVar, dir, f, err)
		}
	}
}

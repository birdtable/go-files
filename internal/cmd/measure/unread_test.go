package main

import (
	"bytes"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// loop is the lint task's loop in Taskfile.yml as unread reads it, for the systems given.
func loop(systems string) string {
	return "tasks:\n  lint:\n    cmds:\n      - cmd: |\n          for os in " + systems + "; do\n            run\n          done\n"
}

// What it costs when this fails: a file the go command builds and golangci-lint does not read,
// assembly, C, a Go file importing C, or one no lint run reads on amd64 or arm64 (built only for a
// system lint's loop leaves out, only with race or cgo, or only on one architecture), makes a raw
// system call or binds a runtime symbol past every row of the register, and every tier passes it.
func TestUnreadRefusesEachFileTheGoCommandBuildsAndLintDoesNotRead(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if code := run([]string{"unread"}, moduleRoot(), &out); code != 0 {
		t.Errorf("unread exited %d on go-files, and want 0: %s", code, &out)
	}
	pass := map[string]string{
		"Taskfile.yml":           loop("linux darwin windows freebsd"),
		"a/a.go":                 "package a\n",
		"a/a_unix.go":            "//go:build unix\n\npackage a\n",
		"a/a_freebsd.go":         "package a\n",
		"a/b_linux_test.go":      "package a\n",
		"a/c.go":                 "//go:build (linux || darwin) && !windows\n\npackage a\n",
		"a/ios_x.go":             "package a\n",
		"a/d.go":                 "package a\n\nimport \"os\"\n\n// netbsd in a comment after the package clause is no constraint.\n//go:build netbsd\nvar _ = os.Args\n",
		"a/e.go":                 "//go:build unix && !openbsd\n\npackage a\n",
		"a/f.go":                 "//go:build !race && !cgo && gc && go1.21\n\npackage a\n",
		"a/g.go":                 "//go:build amd64 || arm64\n\npackage a\n",
		"a/h.go":                 "//go:build !linux && !darwin && !windows\n\npackage a\n",
		"a/notes.txt":            "x\n",
		"a/x.ss":                 "x\n",
		"a/s":                    "x\n",
		".git/objects/x.s":       "x\n",
		".git/hooks/y_netbsd.go": "package y\n",
	}
	out.Reset()
	if code := run([]string{"unread"}, tree(t, pass), &out); code != 0 || out.Len() != 0 {
		t.Errorf("unread exited %d on a tree lint reads whole, and want 0 and nothing said: %s", code, &out)
	}
	refused := map[string]string{
		"a/testdata/prog/t.s":       "x\n",
		".hidden/h.c":               "x\n",
		"_x/y.sx":                   "x\n",
		"a/cgo.go":                  "package a\n\nimport \"C\"\n",
		"a/cgo2.go":                 "package a\n\nimport (\n\t\"C\"\n\t\"os\"\n)\n\nvar _ = os.Args\n",
		"a/x_netbsd.go":             "package a\n",
		"a/x_openbsd_arm64_test.go": "package a\n",
		"a/x_solaris_test.go":       "package a\n",
		"a/t.go":                    "//go:build netbsd\n\npackage a\n",
		"a/t2.go":                   "//go:build !linux && !darwin && !freebsd && !windows\n\npackage a\n",
		"a/r.go":                    "//go:build race\n\npackage a\n",
		"a/r2.go":                   "//go:build cgo\n\npackage a\n",
		"a/z_amd64.go":              "package a\n",
		"a/z_arm64_test.go":         "package a\n",
		"a/k.go":                    "//go:build !amd64\n\npackage a\n",
		"a/gccgo.go":                "//go:build gccgo\n\npackage a\n",
		"a/ig.go":                   "//go:build ignore\n\npackage a\n",
		"a/badc.go":                 "//go:build linux &&\n\npackage a\n",
		"a/t3.go":                   "// +build illumos\n\npackage a\n",
		"a/bad.go":                  "package\n",
	}
	for _, ext := range []string{".s", ".S", ".sx", ".c", ".cc", ".cpp", ".cxx", ".m", ".h", ".hh", ".hpp", ".hxx", ".f", ".F", ".for", ".f90", ".swig", ".swigcxx", ".syso"} {
		refused["a/x"+ext] = "x\n"
	}
	for f, text := range refused {
		files := map[string]string{f: text}
		maps.Copy(files, pass)
		out.Reset()
		if code := run([]string{"unread"}, tree(t, files), &out); code != 1 || !strings.Contains(out.String(), "\n"+f+": ") {
			t.Errorf("unread exited %d with %s, and want 1 naming it: %s", code, f, &out)
		}
	}
	files := maps.Clone(pass)
	files["Taskfile.yml"] = loop("linux darwin windows")
	out.Reset()
	if code := run([]string{"unread"}, tree(t, files), &out); code != 1 || !strings.Contains(out.String(), "\na/a_freebsd.go: by its name and no build constraint, lint, run for linux, darwin, windows on amd64 or arm64 with cgo and race off, does not read it\n") {
		t.Errorf("unread exited %d with freebsd out of lint's loop, and want 1 naming a/a_freebsd.go and its having no constraint: %s", code, &out)
	}
	files = maps.Clone(pass)
	files["a/r.go"] = refused["a/r.go"]
	files["a/z_amd64.go"] = refused["a/z_amd64.go"]
	out.Reset()
	if code := run([]string{"unread"}, tree(t, files), &out); code != 1 ||
		!strings.Contains(out.String(), "\na/r.go: by its name and the build constraint \"//go:build race\", lint") ||
		!strings.Contains(out.String(), "\na/z_amd64.go: by its name and no build constraint, lint, run for linux, darwin, windows, freebsd on arm64 with") {
		t.Errorf("unread exited %d with a/r.go and a/z_amd64.go, and want 1 naming each, its constraint, and the architecture it is not read on: %s", code, &out)
	}
	files = maps.Clone(pass)
	files["Taskfile.yml"] = loop("linux darwin windows freebsd netbsd")
	files["a/t2.go"] = refused["a/t2.go"]
	out.Reset()
	if code := run([]string{"unread"}, tree(t, files), &out); code != 0 {
		t.Errorf("unread exited %d with a/t2.go and netbsd in lint's loop, which reads it, and want 0: %s", code, &out)
	}
	for _, task := range []string{"tasks: {}\n", loop("linux") + loop("darwin")} {
		files["Taskfile.yml"] = task
		out.Reset()
		if code := run([]string{"unread"}, tree(t, files), &out); code != 2 {
			t.Errorf("unread exited %d on a Taskfile.yml with no one lint loop, and want 2: %s", code, &out)
		}
	}
}

// What it costs when this fails: a link in the tree, to a folder of .s or cgo files or to one
// outside it, is followed by the go command and not by the walk, so what it reaches passes
// unread.
func TestUnreadRefusesALinkToAFileOrAFolder(t *testing.T) {
	t.Parallel()
	files := map[string]string{"Taskfile.yml": loop("linux darwin windows freebsd"), "a/a.go": "package a\n", "x/y.go": "package x\n"}
	for _, c := range []struct{ name, target string }{
		{"a/l.go", "a.go"},
		{"a/lib", filepath.Join("..", "x")},
		{"a/out", t.TempDir()},
	} {
		dir := tree(t, files)
		plantLink(t, c.target, filepath.Join(dir, filepath.FromSlash(c.name)))
		var out bytes.Buffer
		if code := run([]string{"unread"}, dir, &out); code != 1 || !strings.Contains(out.String(), "\n"+c.name+": a link") {
			t.Errorf("unread exited %d with a link at %s to %s, and want 1 naming it: %s", code, c.name, c.target, &out)
		}
	}
	for _, mode := range []fs.FileMode{fs.ModeNamedPipe, fs.ModeDevice, fs.ModeIrregular, fs.ModeSocket} {
		found, err := Unread(fstest.MapFS{"a/a.go": {Data: []byte("package a\n")}, "a/odd": {Mode: mode}}, []string{"linux"})
		if err != nil || len(found) != 1 || !strings.HasPrefix(found[0], "a/odd: neither a file nor a folder") {
			t.Errorf("Unread of a %v at a/odd: %q, %v, and want it named neither a file nor a folder", mode, found, err)
		}
	}
}

// plantLink makes a symbolic link at link to target. On Windows the test session makes one
// without elevation, as developer mode is on there, so a refusal fails the test like any other.
func plantLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

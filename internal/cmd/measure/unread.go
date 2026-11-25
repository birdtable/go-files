package main

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// linted is the lint task's one loop in Taskfile.yml, whose words are the systems golangci-lint
// reads the module for. It is read from the file, not from a Task var, which the caller's
// environment would set over.
var linted = regexp.MustCompile(`(?m)^ +for os in ([a-z0-9 ]+); do$`)

// arches are the architectures lint may run for. It runs for the host's alone, and the module's
// hosts are amd64 and arm64 (README.md, Systems), so a Go file must be read on each, whichever host
// runs the tier.
func arches() []string { return []string{"amd64", "arm64"} }

// foreign says whether a Go build takes the file name into a package beside the Go code, by its
// ending: .s assembly, .S and .sx assembly the C preprocessor runs on; C, C++, Objective-C, Fortran
// and SWIG sources and headers, which cgo compiles; and .syso objects, linked into the package as
// they are. golangci-lint reads none, and one reaches past every row of the register: a raw system
// call, a libc trampoline or a runtime symbol, with no import of syscall or unsafe to show it.
func foreign(name string) bool {
	switch path.Ext(name) {
	case ".s", ".S", ".sx",
		".c", ".cc", ".cpp", ".cxx", ".m",
		".h", ".hh", ".hpp", ".hxx",
		".f", ".F", ".for", ".f90",
		".swig", ".swigcxx", ".syso":
		return true
	}
	return false
}

// Unread names each file beneath the module's root that the go command can build and
// golangci-lint, run for the systems lint names, does not read, with why: a link, to a file or a
// folder, and any other name neither a file nor a folder, whose target the walk does not read
// while the go command follows it; a file foreign names; a Go file that imports C, which lint, run
// with cgo off, skips; and a Go file that lint, run for no system of lint on one of arches, reads,
// by go/build's own match of its name and build constraint. It reads every file whatever its build
// tags, testdata and folders whose names start with a dot or _ included, since a test builds a
// program of testdata and a path names any folder, and skips .git alone.
func Unread(fsys fs.FS, lint []string) ([]string, error) {
	var found []string
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if t := d.Type(); t&fs.ModeSymlink != 0 {
			found = append(found, name+": a link, which the go command follows and this walk does not, so a folder or file it reaches is read by no check")
			return nil
		} else if !t.IsRegular() {
			found = append(found, name+": neither a file nor a folder (on Windows, a junction is one), which the go command may follow and this walk does not")
			return nil
		}
		if foreign(name) {
			found = append(found, name+": a "+path.Ext(name)+" file, which the go command builds and lint does not read")
			return nil
		}
		if path.Ext(name) != ".go" {
			return nil
		}
		src, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		found = append(found, goFile(fsys, name, src, lint)...)
		return nil
	})
	return found, err
}

// goFile says why lint does not read the Go file name in fsys, whose text is src, each reason a
// line.
func goFile(fsys fs.FS, name string, src []byte, lint []string) []string {
	f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return []string{name + ": lint cannot read what does not parse: " + err.Error()}
	}
	var why []string
	for _, imp := range f.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err == nil && p == "C" {
			why = append(why, name+": imports C, and lint, run with cgo off, does not read it")
		}
	}
	var missed []string
	for _, goarch := range arches() {
		read, err := readOn(fsys, name, lint, goarch)
		if err != nil {
			return append(why, name+": lint cannot read it: "+err.Error())
		}
		if !read {
			missed = append(missed, goarch)
		}
	}
	if len(missed) > 0 {
		why = append(why, fmt.Sprintf("%s: by its name and %s, lint, run for %s on %s with cgo and race off, does not read it",
			name, buildLine(f), strings.Join(lint, ", "), strings.Join(missed, " or ")))
	}
	return why
}

// buildLine names the build constraint of the Go file f, the lines before its package clause, or
// says it has none.
func buildLine(f *ast.File) string {
	var lines []string
	for _, g := range f.Comments {
		if g.Pos() > f.Package {
			break
		}
		for _, c := range g.List {
			if constraint.IsGoBuild(c.Text) || constraint.IsPlusBuild(c.Text) {
				lines = append(lines, strconv.Quote(c.Text))
			}
		}
	}
	if len(lines) == 0 {
		return "no build constraint"
	}
	return "the build constraint " + strings.Join(lines, " and ")
}

// readOn says whether lint, run for some system of lint on goarch, reads the Go file name in fsys:
// go/build's own match of its name and build constraint against the tags lint runs with, which
// are the system, goarch, unix where the system is one, gc, the toolchain's release tags and
// experiments, and neither cgo nor race, both of which the test member sets.
func readOn(fsys fs.FS, name string, lint []string, goarch string) (bool, error) {
	ctxt := build.Default
	ctxt.GOARCH, ctxt.Compiler, ctxt.CgoEnabled, ctxt.BuildTags, ctxt.UseAllFiles = goarch, "gc", false, nil, false
	ctxt.ToolTags = nil
	for _, tag := range build.Default.ToolTags {
		if strings.HasPrefix(tag, "goexperiment.") {
			ctxt.ToolTags = append(ctxt.ToolTags, tag)
		}
	}
	ctxt.JoinPath = path.Join
	ctxt.OpenFile = func(name string) (io.ReadCloser, error) { return fsys.Open(name) }
	for _, goos := range lint {
		ctxt.GOOS = goos
		if ok, err := ctxt.MatchFile(path.Dir(name), path.Base(name)); err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

// unread refuses the tree beneath root when it holds a file Unread names, naming each, for the
// systems the lint task's loop in root's Taskfile.yml names.
func unread(root *os.Root) (string, int) {
	task, err := root.ReadFile("Taskfile.yml")
	if err != nil {
		return err.Error() + "\n", 2
	}
	loops := linted.FindAllSubmatch(task, -1)
	if len(loops) != 1 {
		return fmt.Sprintf("Taskfile.yml has %d lines of the lint task's loop, `for os in <systems>; do`, and want one\n", len(loops)), 2
	}
	lint := strings.Fields(string(loops[0][1]))
	found, err := Unread(root.FS(), lint)
	if err != nil {
		return err.Error() + "\n", 2
	}
	if len(found) == 0 {
		return "", 0
	}
	return "the go command builds each of these and golangci-lint, run for " + strings.Join(lint, ", ") +
		", does not read it, so it passes every row of the register unread, and go-files holds none:\n" +
		strings.Join(found, "\n") + "\n", 1
}

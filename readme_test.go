package gofiles_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// shown is each function a README shows, by the README, in its order: the file it is in and its
// name. A README.md of the root or a package folder that is not here shows no Go block.
func shown() map[string][][2]string {
	return map[string][][2]string{
		"README.md": {
			{"write/example_test.go", "ExampleFile"},
			{"lock/example_test.go", "ExampleTake"},
		},
		"lock/README.md": {
			{"lock/example_test.go", "sweep"},
			{"lock/example_test.go", "ExampleTake_sweep"},
		},
		"appdirs/README.md": {
			{"appdirs/example_test.go", "cacheIn"},
		},
	}
}

// goBlock is a fenced Go block of a README, without its fences.
var goBlock = regexp.MustCompile("(?s)\n```go\n(.*?)\n```\n")

// TestTheREADMEsShowTheExamplesAsTheyAre fails when the Go blocks of a README.md, in the root or
// a package folder, are not the functions shown lists for it, in its order, each from func to its
// closing brace: a README's Go is what go test runs, an Example checked against its Output line.
func TestTheREADMEsShowTheExamplesAsTheyAre(t *testing.T) {
	t.Parallel()
	readmes, err := fs.Glob(os.DirFS("."), "*/README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range append([]string{"README.md"}, readmes...) {
		readme, err := os.ReadFile(filepath.FromSlash(r))
		if err != nil {
			t.Fatal(err)
		}
		want := shown()[r]
		blocks := goBlock.FindAllStringSubmatch(string(readme), -1)
		if len(blocks) != len(want) {
			t.Errorf("%s has %d Go blocks, want %d, one for each of %v", r, len(blocks), len(want), want)
			continue
		}
		for i, s := range want {
			fn := example(t, s[0], s[1])
			if blocks[i][1] != fn {
				t.Errorf("%s's Go block %d is not %s in %s:\n--- %s\n%s\n--- %s\n%s", r, i+1, s[1], s[0], r, blocks[i][1], s[0], fn)
			}
		}
	}
}

// example is the source of the function name in file, from func to its closing brace.
func example(t *testing.T, file, name string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.FromSlash(file))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		if d, ok := d.(*ast.FuncDecl); ok && d.Recv == nil && d.Name.Name == name {
			return string(src[fset.Position(d.Pos()).Offset:fset.Position(d.End()).Offset])
		}
	}
	t.Fatalf("%s has no func %s", file, name)
	return ""
}

// link is a Markdown link's target, or an HTML src or srcset; code is a fenced block or a code
// span, where a call such as Index[T](name) reads as a link and is not one.
var (
	link = regexp.MustCompile(`\]\(([^)\s]+)\)|\bsrc(?:set)?="([^"]+)"`)
	code = regexp.MustCompile("(?s)```.*?```|`[^`]*`")
)

// TestEveryREADMELinkIntoTheTreeResolves fails for a link in a README.md, in the root or a
// package folder, to a file or folder of the tree that is not there. A link that names a scheme
// or only an anchor is not the tree's and is not read.
func TestEveryREADMELinkIntoTheTreeResolves(t *testing.T) {
	t.Parallel()
	readmes, err := fs.Glob(os.DirFS("."), "*/README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range append([]string{"README.md"}, readmes...) {
		b, err := os.ReadFile(r)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, m := range link.FindAllStringSubmatch(code.ReplaceAllString(string(b), ""), -1) {
			target, _, _ := strings.Cut(m[1]+m[2], "#")
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			n++
			if _, err := os.Stat(filepath.Join(filepath.Dir(r), filepath.FromSlash(target))); err != nil {
				t.Errorf("%s links to %s, which is not in the tree: %v", r, m[1]+m[2], err)
			}
		}
		t.Logf("%s: %d links into the tree", r, n)
	}
}

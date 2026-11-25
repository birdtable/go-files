package trust

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// No string literal in a non-test Go file of trust names a field of Who, for every system's files
// alike: what a refusal says reaches Unsafe.Observed, Unsafe.Error and an exported error's text,
// which a product shows as it is, and a field's name is in the doc comments and the README.
//
// What it costs when this fails: a person is shown "Who.Mounts", a Go field and no fact of their
// machine or setting they can find, and a product that wants its own words compares text instead
// of matching ErrMount, ErrMountEntry, ErrInside or ErrOverflow.
func TestNoTextOfTheLibraryNamesAFieldOfWho(t *testing.T) {
	t.Parallel()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	field := regexp.MustCompile(`Who\.[A-Z]`)
	fset := token.NewFileSet()
	files, literals := 0, 0
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files++
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				literals++
				if field.MatchString(lit.Value) {
					t.Errorf("%s: %s names a field of Who; say what it stands for, as \"the mounts named as trusted\"", fset.Position(lit.Pos()), lit.Value)
				}
			}
			return true
		})
	}
	if files < 10 || literals < 100 {
		t.Fatalf("read %d non-test files and %d string literals, and want the package's, at least 10 and 100", files, literals)
	}
}

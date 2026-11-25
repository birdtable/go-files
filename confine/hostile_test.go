package confine_test

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/birdtable/go-files/confine"
	"github.com/birdtable/go-files/filestest"
)

// refusal is why path is not portable, as filestest.AllPortable says it: each name's refusal by
// Portable, in turn, and then Local's of the whole path.
func refusal(path string) error {
	for name := range strings.SplitSeq(path, "/") {
		if _, err := confine.Portable(name); err != nil {
			return err
		}
	}
	_, err := confine.Local(path)
	return err
}

// Each of filestest's hostile names is refused, by Portable a name at a time or by Local, with an
// error that says the rule the list names: the rows from go-git's own tests of .git, its 8.3 name
// and its NTFS and HFS+ disguises among them. A slash in one name is
// refused too.
//
// What it costs when this fails: a name made on Linux is CON's device on Windows, or .git to
// macOS, or the same file as another on a volume that folds case, and a checkout or a copy writes
// where it was never meant to; or the list a product's tests read says a name is refused that the
// library takes.
func TestPortableRefusesEachNameByItsRule(t *testing.T) {
	t.Parallel()
	list := filestest.HostileNames()
	if len(list) < 100 {
		t.Fatalf("the list holds %d names, and want go-git's rows and more", len(list))
	}
	for _, h := range list {
		err := refusal(h.Path)
		if h.Why == "" || err == nil || !strings.Contains(err.Error(), h.Why) {
			t.Errorf("%+q: %v, and want a refusal that says %q", h.Path, err, h.Why)
		}
		if err != nil && !errors.Is(err, fs.ErrInvalid) && !errors.Is(err, confine.ErrOutside) {
			t.Errorf("%+q: %v, and want it to match fs.ErrInvalid or ErrOutside", h.Path, err)
		}
	}
	if key, err := confine.Portable("a/b"); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf(`Portable("a/b") = %q, %v, and want a slash in one name refused`, key, err)
	}
}

// Every reason Portable and Local give, as their code writes it, is said by the refusal of a name
// on filestest's list, so a rule added without an entry, or an entry taken out that was its rule's
// last, is red, and a rule dropped leaves its entries unrefused above.
//
// What it costs when this fails: the one list a product's tests read misses a name the library
// refuses, and a product's own check of its names lets that name through.
func TestEveryRuleOfPortableAndLocalHasAHostileName(t *testing.T) {
	t.Parallel()
	rules := slices.Concat(
		reasons(t, "portable.go", "Portable", "unportable", "unportableForm", "unportableRune"),
		reasons(t, "confine.go", "Local"))
	if len(rules) < 15 {
		t.Fatalf("read %d reasons, %q, and want Portable's and Local's every one", len(rules), rules)
	}
	var said []string
	for _, h := range filestest.HostileNames() {
		if err := refusal(h.Path); err != nil {
			said = append(said, err.Error())
		}
	}
	for _, rule := range rules {
		if !slices.ContainsFunc(said, func(s string) bool { return strings.Contains(s, rule) }) {
			t.Errorf("no hostile name is refused saying %q", rule)
		}
	}
}

// reasons is each reason the named functions of file give, from its source: a string a return
// gives, the left of a string a return joins, and the format of a fmt.Sprintf or fmt.Errorf, each
// cut at its first verb, with Portable's and Local's own wording around the reason trimmed.
func reasons(t *testing.T, file string, funcs ...string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	take := func(e ast.Expr) {
		for {
			b, ok := e.(*ast.BinaryExpr)
			if !ok {
				break
			}
			e = b.X
		}
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			t.Fatal(err)
		}
		for _, prefix := range []string{"%q is not a portable name: ", "%q: ", "%q "} {
			s = strings.TrimPrefix(s, prefix)
		}
		s, _, _ = strings.Cut(strings.TrimSuffix(s, ": %w"), "%")
		if s = strings.TrimRight(s, " ,"); s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	found := 0
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || !slices.Contains(funcs, fd.Name.Name) {
			continue
		}
		found++
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.ReturnStmt:
				for _, r := range n.Results {
					take(r)
				}
			case *ast.CallExpr:
				if sel, ok := n.Fun.(*ast.SelectorExpr); ok && (sel.Sel.Name == "Sprintf" || sel.Sel.Name == "Errorf") && len(n.Args) > 0 {
					take(n.Args[0])
				}
			}
			return true
		})
	}
	if found != len(funcs) {
		t.Fatalf("%s holds %d of the functions %q", file, found, funcs)
	}
	return out
}

package trust_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// rawName is every raw system call the register's no-raw-call row in .golangci.yml names.
var rawName = regexp.MustCompile(`^(AllThreads|Raw)?Syscall(6|9|12|15|18|N)?(NoError)?$`)

// oneRawCall refuses src, a Go file, unless it imports golang.org/x/sys/unix and syscall, if at all,
// by their own names, names a raw system call of either once, and that once is the call the lint
// row grants: unix.Syscall6(unix.SYS_GETATTRLIST, ...) with unix.FSOPT_NOFOLLOW as its options. The
// lint row that excepts trust/maclist_darwin.go matches that call's line; this holds the rest.
func oneRawCall(src []byte) error {
	f, err := parser.ParseFile(token.NewFileSet(), "maclist_darwin.go", src, parser.SkipObjectResolution)
	if err != nil {
		return err
	}
	for _, im := range f.Imports {
		p, err := strconv.Unquote(im.Path.Value)
		if err != nil {
			return err
		}
		if (p == "syscall" || p == "golang.org/x/sys/unix") && im.Name != nil {
			return fmt.Errorf("it imports %s as %s", p, im.Name.Name)
		}
	}
	var named []*ast.SelectorExpr
	granted := map[*ast.SelectorExpr]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			if x, ok := n.X.(*ast.Ident); ok && (x.Name == "unix" || x.Name == "syscall") && rawName.MatchString(n.Sel.Name) {
				named = append(named, n)
			}
		case *ast.CallExpr:
			// Syscall6's trap and six arguments: getattrlist's path, list, buffer, size and options.
			if sel, ok := n.Fun.(*ast.SelectorExpr); ok && isUnix(sel, "Syscall6") && len(n.Args) == 7 &&
				isUnix(n.Args[0], "SYS_GETATTRLIST") && isUnix(n.Args[5], "FSOPT_NOFOLLOW") {
				granted[sel] = true
			}
		}
		return true
	})
	if len(named) != 1 {
		return fmt.Errorf("it names %d raw system calls of unix or syscall, and want 1", len(named))
	}
	if !granted[named[0]] {
		return fmt.Errorf("its raw call, %s.%s, is not unix.Syscall6(unix.SYS_GETATTRLIST, ...) with unix.FSOPT_NOFOLLOW",
			named[0].X, named[0].Sel.Name)
	}
	return nil
}

// isUnix says whether e is unix.<name>.
func isUnix(e ast.Expr, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == "unix" && sel.Sel.Name == name
}

// What it costs when this fails: the lint row that lets trust/maclist_darwin.go make its one
// raw call lets a second through, a write call among them, and no gate sees it.
func TestTheMacListFileMakesOnlyItsOneRawCall(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("maclist_darwin.go")
	if err != nil {
		t.Fatal(err)
	}
	if err := oneRawCall(src); err != nil {
		t.Fatalf("maclist_darwin.go: %v", err)
	}
	const call = "unix.Syscall6(unix.SYS_GETATTRLIST, "
	if strings.Count(string(src), call) != 1 {
		t.Fatalf("maclist_darwin.go holds %q %d times, and want once, for the plants below", call, strings.Count(string(src), call))
	}
	// Each plant is the file with one change; each must be refused.
	for _, p := range []struct{ name, src string }{
		{"a second call, of another number", string(src) + "\nfunc planted() { unix.Syscall(unix.SYS_SETATTRLIST, 0, 0, 0) }\n"},
		{"a second call, the same", string(src) + "\nfunc planted() { unix.Syscall6(unix.SYS_GETATTRLIST, 0, 0, 0, 0, unix.FSOPT_NOFOLLOW, 0) }\n"},
		{"a raw call kept in a variable", string(src) + "\nvar planted = unix.RawSyscall\n"},
		{"each numbered form", string(src) + "\nfunc planted() { unix.Syscall9(0, 0, 0, 0, 0, 0, 0, 0, 0, 0) }\n"},
		{"the NoError form", string(src) + "\nfunc planted() { unix.RawSyscallNoError(0, 0, 0, 0) }\n"},
		{"the call made without FSOPT_NOFOLLOW", strings.Replace(string(src), "unix.FSOPT_NOFOLLOW", "0", 1)},
		{"the call made of another number", strings.Replace(string(src), call, "unix.Syscall6(unix.SYS_SETATTRLIST, ", 1)},
		{"the call made through syscall", strings.Replace(string(src), call, "syscall.Syscall6(unix.SYS_GETATTRLIST, ", 1)},
		{"the options moved to another argument", strings.Replace(string(src), "unix.FSOPT_NOFOLLOW, 0)", "0, unix.FSOPT_NOFOLLOW)", 1)},
		{"a raw call of syscall", string(src) + "\nfunc planted() { syscall.Syscall(0, 0, 0, 0) }\n"},
		{"syscall imported by another name", strings.Replace(string(src), "\t\"syscall\"", "\ts \"syscall\"", 1)},
		{"unix imported by another name", strings.Replace(string(src), "\"golang.org/x/sys/unix\"", "u \"golang.org/x/sys/unix\"", 1)},
	} {
		if p.src == string(src) {
			t.Errorf("%s: the plant changed nothing", p.name)
			continue
		}
		if err := oneRawCall([]byte(p.src)); err == nil {
			t.Errorf("%s: passed", p.name)
		}
	}
}

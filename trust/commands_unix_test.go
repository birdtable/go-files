//go:build unix

package trust

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test runs each step of a fix as its argv, as a person runs it; the module has no runner
	"path/filepath"
	"syscall"
	"testing"
)

// run starts each step of fix's commands as its argv, with no shell, and fails the test when fix
// has none or a step needs root, which the test tier never is.
func run(t *testing.T, fix Fix) {
	t.Helper()
	steps, ok := fix.Commands()
	if !ok {
		t.Fatalf("%+v: no commands", fix)
	}
	for _, s := range steps {
		if s.Admin {
			t.Fatalf("%q needs root, and the test tier is not root", s.Argv)
		}
		if out, err := exec.CommandContext(t.Context(), s.Argv[0], s.Argv[1:]...).CombinedOutput(); err != nil { //nolint:gosec // the argv is Commands' own, and running it is the test
			t.Fatalf("%q: %v: %s", s.Argv, err, out)
		}
	}
}

// What it costs when this fails: a person runs the steps a refusal's fix gives, and the name is
// still refused, or a name that holds a space, a quote, a dollar or a leading dash is split, read
// as a switch or expanded, so another name is changed.
func TestEachStepOfAFixRunAsItsArgvMakesTheNamePass(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	for _, w := range []string{"a", "b"} {
		mk(t, filepath.Join(d, w), File, 0o666)
	}
	for _, c := range []struct {
		name string
		k    Kind
		perm fs.FileMode
	}{
		{"-R a b", File, 0o666},
		{`$HOME "q" 'r'`, Folder, 0o777},
		{"key; x", Key, 0o644},
		{"program`x`", Program, 0o644},
		{"private\\", PrivateFolder, 0o755},
	} {
		p := filepath.Join(d, c.name)
		mk(t, p, c.k, c.perm)
		_, err := Path(p, c.k, Who{})
		u, ok := errors.AsType[*Unsafe](err)
		if !ok || u.Fix.Path != p {
			t.Errorf("%q at mode %o: %v, and want a refusal whose fix names it", c.name, c.perm, err)
			continue
		}
		run(t, u.Fix)
		if _, err := Path(p, c.k, Who{}); err != nil {
			t.Errorf("%q after %+v: %v", c.name, u.Fix, err)
		}
	}
	for _, w := range []string{"a", "b"} {
		if _, err := Path(filepath.Join(d, w), File, Who{}); !errors.Is(err, ErrOthersWrite) {
			t.Errorf("%s, a word of a name a step named: %v, and want it still refused", w, err)
		}
	}
}

// What it costs when this fails: a person runs the line a program shows to give a name to this
// account, and chown refuses the uid's number or gives the name, or what a link there names, to
// another account. The test tier is not root, so the step runs on a name this
// account owns already, which chown lets its owner do, and a link's own owner is read after it.
func TestTheStepThatGivesANameToThisAccountRunsAsItsArgv(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	me, err := Account()
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(d, "-R mine $x")
	mk(t, f, File, 0o600)
	l := filepath.Join(d, "link")
	if err := os.Symlink(f, l); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{f, l} {
		steps, ok := Fix{Path: p, Owner: me}.Commands()
		if !ok || len(steps) != 1 || !steps[0].Admin {
			t.Fatalf("%s: %s, %v, and want one step that needs root", p, shown(steps), ok)
		}
		s := steps[0].Argv
		prog := s[0]
		if _, err := exec.LookPath(prog); err != nil {
			prog = filepath.Join("/usr/sbin", prog) // macOS keeps chown there, which a person's PATH holds and a test's need not
		}
		if out, err := exec.CommandContext(t.Context(), prog, s[1:]...).CombinedOutput(); err != nil { //nolint:gosec // the argv is Commands' own, and running it is the test
			t.Fatalf("%q: %v: %s", s, err, out)
		}
		st, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("uid:%d", st.Sys().(*syscall.Stat_t).Uid); got != me {
			t.Errorf("%s after %q: owned by %s, and want %s", p, s, got, me)
		}
	}
}

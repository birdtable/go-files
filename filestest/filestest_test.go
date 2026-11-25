package filestest_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test starts the programs WriteProgram wrote, and this test binary again as a second process taking private folders; the module has no runner
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/birdtable/go-files/filestest"
	"github.com/birdtable/go-files/trust"
	"github.com/birdtable/go-files/trust/trusttest"
)

// recorder is a testing.TB that keeps what a helper says and the cleanups it asks for, so a test
// reads a failure the helper would have made. Its TB may be nil, in a helper process: the helpers
// call Helper, Errorf, Fatalf and Cleanup alone.
type recorder struct {
	testing.TB
	failed   bool
	said     []string
	cleanups []func()
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.failed = true
	r.said = append(r.said, fmt.Sprintf(format, args...))
}

func (r *recorder) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	runtime.Goexit()
}

func (r *recorder) Cleanup(f func()) { r.cleanups = append(r.cleanups, f) }

// end runs the cleanups, the last asked for first, as a test's end does.
func (r *recorder) end() {
	for _, f := range slices.Backward(r.cleanups) {
		f()
	}
	r.cleanups = nil
}

// says is whether the recorder said anything holding s.
func (r *recorder) says(s string) bool { return strings.Contains(strings.Join(r.said, "\n"), s) }

// run is f called with r, once it has returned or ended, and answers r.
func (r *recorder) run(f func(r *recorder)) *recorder {
	done := make(chan struct{})
	go func() {
		defer close(done)
		f(r)
	}()
	<-done
	return r
}

// judged is trust.Path's answer for path as a private folder, with Who.Overflow in a Linux user
// namespace that maps this account and not root, and without it where trust refuses that, as
// Private takes its folder.
func judged(path string) error {
	_, err := trust.Path(path, trust.PrivateFolder, trust.Who{Overflow: true})
	if errors.Is(err, trust.ErrOverflow) {
		_, err = trust.Path(path, trust.PrivateFolder, trust.Who{})
	}
	return err
}

// Private's folder passes the trust walk of a private folder, where on Unix t.TempDir's is
// refused, holds nothing, and two calls answer two folders.
//
// What it costs when this fails: a test that hands a folder to a trust walk takes t.TempDir and is
// refused, or names a mode or os.MkdirTemp to get past it, which a product's tests ban.
func TestPrivatePassesTheWalkTheTestsOwnFolderIsRefused(t *testing.T) {
	t.Parallel()
	switch err := judged(t.TempDir()); {
	case !tempDirRefused:
		// Windows: a default profile's temp folder holds this account, SYSTEM and Administrators
		// alone, so t.TempDir's folder may pass there.
		t.Logf("t.TempDir's folder, which may pass here: %v", err)
	case os.Geteuid() == 0:
		t.Logf("root, whom every rule trusts, passes t.TempDir's folder too")
	case err == nil:
		t.Errorf("t.TempDir's folder passes the walk, so this test says nothing of Private")
	}
	a, b := filestest.Private(t), filestest.Private(t)
	for _, p := range []string{a, b} {
		if err := judged(p); err != nil {
			t.Errorf("Private's %s: %v, and want it judged this account's alone", p, err)
		}
		if es, err := os.ReadDir(p); err != nil || len(es) != 0 {
			t.Errorf("Private's %s holds %v (%v), and want it empty", p, es, err)
		}
		if strings.Contains(p, t.Name()) {
			t.Errorf("Private's %s holds the test's name", p)
		}
	}
	if a == b {
		t.Errorf("two calls of Private answered %s both times", a)
	}
}

// Private, from many parallel tests and from two test binaries at once, answers a folder of its
// own to each call.
//
// What it costs when this fails: two tests, or two packages' test binaries under go test ./...,
// share one private folder, and each reads or removes the other's files.
func TestPrivateGivesEachCallAFolderOfItsOwn(t *testing.T) {
	t.Parallel()
	const tests, each, processes, perProcess = 16, 4, 2, 32
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var cmds []*exec.Cmd
	var outs, errs []*bytes.Buffer
	for range processes {
		cmd := exec.CommandContext(context.WithoutCancel(t.Context()), exe, privateArg, strconv.Itoa(perProcess)) //nolint:gosec // this test binary again, with the test's own arguments
		out, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		cmd.Stdout, cmd.Stderr = out, stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds, outs, errs = append(cmds, cmd), append(outs, out), append(errs, stderr)
	}
	var mu sync.Mutex
	var all []string
	// The parallel subtests run once this function returns, and this cleanup once they end.
	t.Cleanup(func() {
		for i, cmd := range cmds {
			if err := cmd.Wait(); err != nil {
				t.Errorf("helper process %d: %v: %s", i, err, errs[i])
				continue
			}
			lines := bufio.NewScanner(outs[i])
			for lines.Scan() {
				all = append(all, lines.Text())
			}
		}
		seen := map[string]bool{}
		for _, p := range all {
			if seen[p] {
				t.Errorf("two calls of Private answered %s", p)
			}
			seen[p] = true
		}
		if want := tests*each + processes*perProcess; len(all) != want {
			t.Errorf("%d folders answered, and want %d", len(all), want)
		}
	})
	for i := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			for range each {
				p := filestest.Private(t)
				mu.Lock()
				all = append(all, p)
				mu.Unlock()
			}
		})
	}
}

// At the test's end Private's folder is gone, with a file and a folder left in it.
//
// What it costs when this fails: each test run leaves its private folders, and what a test wrote
// there, in the account's temp folder for good.
func TestPrivateIsRemovedAtTheTestsEndWithAllItHolds(t *testing.T) {
	t.Parallel()
	var p string
	r := (&recorder{TB: t}).run(func(r *recorder) {
		p = filestest.Private(r)
		filestest.WriteFile(r, filepath.Join(p, "left"), []byte("x"))
		if err := os.Mkdir(filepath.Join(p, "folder"), 0o700); err != nil {
			r.Fatalf("%v", err)
		}
		filestest.WriteFile(r, filepath.Join(p, "folder", "left"), []byte("x"))
	})
	if r.failed || p == "" {
		t.Fatalf("Private and the writes in it: %q", r.said)
	}
	r.end()
	if r.failed {
		t.Errorf("the test's end: %q, and want Private's folder removed with no failure", r.said)
	}
	if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Private's %s after the test's end: %v, and want it gone", p, err)
	}
}

// WriteFile to a path whose folder is missing fails the test, says the path, and makes no folder.
//
// What it costs when this fails: a test writes its file in a folder it never meant, or fails
// without saying which file it could not write.
func TestWriteFileFailsTheTestSayingThePathWhoseFolderIsMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, write := range map[string]func(testing.TB, string, []byte){"WriteFile": filestest.WriteFile, "WriteProgram": filestest.WriteProgram} {
		p := filepath.Join(dir, "missing", name)
		r := (&recorder{TB: t}).run(func(r *recorder) { write(r, p, []byte("x")) })
		if !r.failed || !r.says(p) {
			t.Errorf("%s to %s, whose folder is missing: failed %v, said %q, and want a failure saying the path", name, p, r.failed, r.said)
		}
	}
	if es, err := os.ReadDir(dir); err != nil || len(es) != 0 {
		t.Errorf("the folder holds %v (%v), and want nothing made", es, err)
	}
}

// WriteFile's file is the library's private file and WriteProgram's its private program, 0600 and
// 0700 on Unix and the owner-only list on Windows, holding what was written.
//
// What it costs when this fails: a test's file, which may hold a token, is readable by other
// accounts, or a test names a mode to make it otherwise.
func TestWriteFileAndWriteProgramWriteTheLibrarysPrivateModes(t *testing.T) {
	t.Parallel()
	dir := filestest.Private(t)
	file, prog := filepath.Join(dir, "settings"), filepath.Join(dir, "prog")
	filestest.WriteFile(t, file, []byte("a = 1\n"))
	filestest.WriteProgram(t, prog, []byte("#!/bin/sh\n"))
	trusttest.Perm(t, file, 0o600)
	trusttest.Perm(t, prog, 0o700)
	for p, want := range map[string]string{file: "a = 1\n", prog: "#!/bin/sh\n"} {
		if b, err := os.ReadFile(p); err != nil || string(b) != want {
			t.Errorf("%s holds %q (%v), and want %q", p, b, err, want)
		}
	}
}

// A program WriteProgram wrote starts at once, each of many, while other goroutines write and
// start theirs, with no "text file busy".
//
// What it costs when this fails: a test that writes a program and runs it fails now and then,
// when a process another goroutine started holds the program open for writing.
func TestAProgramWriteProgramWroteStartsAtOnceWhileOthersFork(t *testing.T) {
	t.Parallel()
	content, args, goroutines, rounds := program(t)
	dir := t.TempDir()
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Go(func() {
			for i := range rounds {
				p := filepath.Join(dir, fmt.Sprintf("prog-%d-%d%s", g, i, exeSuffix))
				filestest.WriteProgram(t, p, content)
				if out, err := exec.CommandContext(t.Context(), p, args...).CombinedOutput(); err != nil { //nolint:gosec // a program this test wrote
					t.Errorf("%s, started at once: %v: %s", p, err, out)
				}
			}
		})
	}
	wg.Wait()
}

// CopyTree copies a file, a program and a folder into a private folder, by a relative src too:
// each holds what it held, the file private and 0600 and the program 0700 on Unix, the owner-only
// list on Windows.
//
// What it costs when this fails: a test copies its fixture tree by hand, with no refusal of a link
// that leads outside, or names a mode to keep a program runnable.
func TestCopyTreeCopiesAFixtureTreeWithThePrivateModes(t *testing.T) {
	t.Parallel()
	from := filestest.Private(t)
	if err := os.Mkdir(filepath.Join(from, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	filestest.WriteFile(t, filepath.Join(from, "sub", "settings"), []byte("a = 1\n"))
	filestest.WriteProgram(t, filepath.Join(from, "prog"), []byte("#!/bin/sh\n"))
	src := from
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, from); err == nil {
			src = rel
		}
	}
	to := filestest.Private(t)
	filestest.CopyTree(t, to, src)
	trusttest.Perm(t, filepath.Join(to, "sub", "settings"), 0o600)
	trusttest.Perm(t, filepath.Join(to, "prog"), 0o700)
	for p, want := range map[string]string{filepath.Join("sub", "settings"): "a = 1\n", "prog": "#!/bin/sh\n"} {
		if b, err := os.ReadFile(filepath.Join(to, p)); err != nil || string(b) != want {
			t.Errorf("%s in the copy holds %q (%v), and want %q", p, b, err, want)
		}
	}
}

// CopyTree into a folder that holds a name the tree holds fails the test, saying both paths, and
// leaves the name as it was.
//
// What it costs when this fails: a test's copy over a folder it reused replaces a file there in
// silence, or fails without saying which tree it could not copy.
func TestCopyTreeFailsTheTestSayingBothPaths(t *testing.T) {
	t.Parallel()
	from, to := filestest.Private(t), filestest.Private(t)
	filestest.WriteFile(t, filepath.Join(from, "a"), []byte("theirs"))
	filestest.WriteFile(t, filepath.Join(to, "a"), []byte("mine"))
	r := (&recorder{TB: t}).run(func(r *recorder) { filestest.CopyTree(r, to, from) })
	if !r.failed || !r.says(from) || !r.says(to) {
		t.Errorf("CopyTree over a name in dst: failed %v, said %q, and want a failure saying %s and %s", r.failed, r.said, from, to)
	}
	if b, err := os.ReadFile(filepath.Join(to, "a")); err != nil || string(b) != "mine" {
		t.Errorf("a in dst holds %q (%v), and want it left", b, err)
	}
}

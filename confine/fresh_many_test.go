package confine_test

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test starts this test binary again as a second process making fresh folders; the module has no runner
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// freshHelperArg starts this test binary as a helper process instead of its tests (TestMain).
const freshHelperArg = "confine-fresh-helper"

// freshHelper makes n fresh folders in dir, and says each name on a line of its stdout. It is the
// helper process's exit code.
func freshHelper(dir, count string) int {
	n, err := strconv.Atoi(count)
	if err == nil {
		err = makeFresh(dir, n, func(name string) {
			fmt.Fprintln(os.Stdout, name) //nolint:forbidigo // the helper says each name on its stdout, the pipe the test reads
		})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err) //nolint:forbidigo // the helper process says why it failed on its stderr, which the test reads
		return 1
	}
	return 0
}

// makeFresh makes n fresh folders in dir with the prefix "job-", handing each name to made.
func makeFresh(dir string, n int, made func(name string)) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	for range n {
		got, name, err := confine.MkdirFresh(root, "job-")
		if err != nil {
			return err
		}
		got.Close()
		made(name)
	}
	return nil
}

// Many MkdirFresh calls at once in one root, from several goroutines and from two processes, each
// make a folder, and no two share a name.
//
// What it costs when this fails: two jobs started together are handed one scratch folder, and each
// reads or removes the other's files, or one fails for a name the other took.
func TestMkdirFreshManyAtOnceShareNoName(t *testing.T) {
	t.Parallel()
	const goroutines, each, processes, perProcess = 16, 16, 2, 128
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var cmds []*exec.Cmd
	var outs []*bytes.Buffer
	for range processes {
		cmd := exec.CommandContext(t.Context(), exe, freshHelperArg, dir, strconv.Itoa(perProcess)) //nolint:gosec // this test binary again, with the test's own arguments
		out := &bytes.Buffer{}
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds, outs = append(cmds, cmd), append(outs, out)
	}
	var mu sync.Mutex
	var all []string
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			err := makeFresh(dir, each, func(name string) {
				mu.Lock()
				all = append(all, name)
				mu.Unlock()
			})
			if err != nil {
				t.Errorf("a goroutine's MkdirFresh: %v", err)
			}
		})
	}
	wg.Wait()
	for i, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("helper process %d: %v: %s", i, err, outs[i])
		}
		lines := bufio.NewScanner(outs[i])
		for lines.Scan() {
			all = append(all, strings.TrimSpace(lines.Text()))
		}
	}
	want := goroutines*each + processes*perProcess
	seen := map[string]bool{}
	for _, name := range all {
		if seen[name] {
			t.Errorf("two calls answered %q", name)
		}
		seen[name] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != want || len(entries) != want {
		t.Errorf("%d names answered and %d folders made, and want %d of each", len(all), len(entries), want)
	}
	for _, e := range entries {
		if !seen[e.Name()] || !e.IsDir() {
			t.Errorf("the root holds %q, which no call answered as its folder", e.Name())
		}
	}
}

//go:build !windows

package write

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A hold the tests let go of is free at once, even while a process forked from the test shares its
// descriptor, as a child that a parallel test forks does until it execs: a LogWriter whose ctx has
// ended, which tries the hold once, then writes.
//
// What it costs when this fails: a test that writes with an ended ctx once its hold is let go fails
// at random under load, when another test's fork lands while the hold is kept (go-files#28).
func TestALetGoHoldIsFreeWhileAChildSharesItsDescriptor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := filepath.Join(dir, "app.log")
	put0(t, log, "old\n")
	f, letGoOf := sharedHold(t, log)
	child := helperCmd(t, "run")
	child.ExtraFiles = []*os.File{f}
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		if err := child.Wait(); err != nil {
			t.Errorf("the child that shared the hold: %v", err)
		}
	}()
	if line, err := bufio.NewReader(stdout).ReadString('\n'); line != "running\n" || err != nil {
		t.Fatalf("the child that shares the hold said %q (%v), and want running", line, err)
	}
	letGoOf()
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	w := logWriterIn(t, canceled, dir, "app.log", 1000)
	wrote(t, w, "line\n", 5)
	holds(t, log, "old\nline\n")
}

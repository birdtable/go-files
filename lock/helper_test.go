package lock

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the tests start this test binary again as the second process a lock keeps out; the module has no runner
	"testing"
)

// helperArg starts this test binary as a helper process instead of its tests
// (TestMain): a second process to hold the lock, or to make a lock file under
// a umask.
const helperArg = "lock-helper"

// helper runs the helper process mode names, and is its exit code:
//
//   - hold <path>: takes the lock, notes {"by":"helper"}, says "held" on its
//     stdout, and holds the lock until its stdin ends;
//   - umask <octal> <path>: takes the lock under that umask (Unix).
func helper(mode string, args []string) int {
	err := fmt.Errorf("unknown helper mode %q %q", mode, args)
	switch {
	case mode == "hold" && len(args) == 1:
		err = hold(args[0])
	case mode == "umask" && len(args) == 2:
		err = umaskTake(args[0], args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err) //nolint:forbidigo // the helper process says why it failed on its stderr, which the test reads
		return 1
	}
	return 0
}

func hold(path string) error {
	l, err := Take(path, path+".holder")
	if err != nil {
		return err
	}
	if err := l.Note(map[string]string{"by": "helper"}); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "held") //nolint:forbidigo // the helper process tells the test it holds the lock on its stdout, the pipe the test reads
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		return err
	}
	return l.Release()
}

// helperCmd is this test binary as a helper process in mode, ended with the test.
func helperCmd(t *testing.T, mode string, args ...string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exec.CommandContext(t.Context(), exe, append([]string{helperArg, mode}, args...)...) //nolint:gosec // this test binary again, with the test's own arguments
}

// holding starts a hold helper on path and returns once it holds the lock,
// with the helper and its stdin, which ends the hold when closed.
func holding(t *testing.T, path string) (*exec.Cmd, io.WriteCloser) {
	t.Helper()
	cmd := helperCmd(t, "hold", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = in.Close()
		_ = cmd.Wait()
	})
	if line, err := bufio.NewReader(out).ReadString('\n'); line != "held\n" {
		_ = in.Close()
		_ = cmd.Wait()
		t.Fatalf("the helper did not take the lock: %q, %v, %s", line, err, stderr.String())
	}
	return cmd, in
}

// plantLink makes a symbolic link at link to target. On Windows the test session makes one
// without elevation, as developer mode is on there, so a refusal fails the test like any other.
func plantLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

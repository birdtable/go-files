package write

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the tests start this test binary again as a writer to kill or a second appender; the module has no runner
	"path/filepath"
	"strconv"
	"testing"
)

// helperArg starts this test binary as a helper process instead of its tests (TestMain).
const helperArg = "write-helper"

// helper runs the helper process mode names, and is its exit code:
//
//   - stop <path>: writes "new" over path, and once the temp file is written and closed, before
//     the rename, says "written" on its stdout and waits for its stdin to end, to be killed;
//   - append <path> <tag> <n> <limit>: appends n lines "<tag> <i>" to path, under the cap limit;
//   - run: says "running" on its stdout and waits for its stdin to end, a program Swap replaces
//     while it runs;
//   - umask <octal> <path> <perm>: writes path with Mode(perm), or no Mode when perm is "-",
//     under that umask (Unix).
//   - umask-log <octal> <path> <perm>: appends a line to the log at path, then one past its cap,
//     with LogMode(perm), or none when perm is "-", under that umask (Unix).
func helper(mode string, args []string) int {
	err := fmt.Errorf("unknown helper mode %q %q", mode, args)
	switch {
	case mode == "stop" && len(args) == 1:
		err = File(args[0], []byte("new"), stepWith(func(at string) {
			if at == "written" {
				fmt.Fprintln(os.Stdout, "written")   //nolint:forbidigo // the helper tells the test where it is on its stdout, the pipe the test reads
				_, _ = io.Copy(io.Discard, os.Stdin) // the test kills it here
			}
		}))
	case mode == "run" && len(args) == 0:
		fmt.Fprintln(os.Stdout, "running")   //nolint:forbidigo // the helper tells the test it runs on its stdout, the pipe the test reads
		_, _ = io.Copy(io.Discard, os.Stdin) // the test ends it by closing its stdin
		err = nil
	case mode == "append" && len(args) == 4:
		err = appendLines(args[0], args[1], args[2], args[3])
	case mode == "umask-log" && len(args) == 3:
		err = umaskLog(args[0], args[1], args[2])
	case mode == "umask" && len(args) == 3:
		err = umaskWrite(args[0], args[1], args[2])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err) //nolint:forbidigo // the helper process says why it failed on its stderr, which the test reads
		return 1
	}
	return 0
}

func appendLines(path, tag, count, most string) error {
	n, err := strconv.Atoi(count)
	if err != nil {
		return err
	}
	limit, err := strconv.ParseInt(most, 10, 64)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	for i := range n {
		if err := Append(context.Background(), root, filepath.Base(path), []byte(tag+" "+strconv.Itoa(i)), limit, Aside(filepath.Base(path)+".1")); err != nil {
			return errors.Join(err, root.Close())
		}
	}
	return root.Close()
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

// plantLink makes a symbolic link at link to target, which a relative target names from link's
// folder, as an os.Root of that folder follows it. On Windows the test session makes one without
// elevation, as developer mode is on there, so a refusal fails the test like any other.
func plantLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// panics is whether f panics.
func panics(f func()) (did bool) {
	defer func() { did = recover() != nil }()
	f()
	return false
}

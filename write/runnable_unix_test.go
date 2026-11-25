//go:build !windows

package write

import (
	"errors"
	"io"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: Check runs the file it was given, to show a Check may start a process under Runnable; the module has no runner
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// prog is a program a test writes and runs: it exits 3, so a run of it is told from a failed start.
const prog = "#!/bin/sh\nexit 3\n"

// ranIt is nil when err is prog's own exit, 3.
func ranIt(err error) error {
	if exit := (*exec.ExitError)(nil); errors.As(err, &exit) && exit.ExitCode() == 3 {
		return nil
	}
	return errors.Join(errors.New("the program did not run to its exit 3"), err)
}

// While a fork holds the fork lock's write side, a Runnable write does not create its temp file;
// once the fork is done, it writes the file at 0700.
//
// What it costs when this fails: a child forked between the temp file's create and its close
// holds it open for writing, and running the file fails with text file busy on Linux until that
// child execs or exits.
func TestARunnableFileIsNotMadeWhileAForkIsMade(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "prog")
	syscall.ForkLock.Lock()
	written := make(chan error, 1)
	go func() { written <- File(path, []byte(prog), Runnable()) }()
	select {
	case err := <-written:
		syscall.ForkLock.Unlock()
		t.Fatalf("the file was written while a fork held the lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	entries, err := os.ReadDir(dir)
	syscall.ForkLock.Unlock()
	if err != nil || len(entries) != 0 {
		t.Errorf("the folder held %v (%v) while a fork held the lock, and want nothing", entries, err)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	holds(t, path, prog)
	permIs(t, path, 0o700)
	alone(t, dir, "prog")
}

// A Runnable file is 0700, or Mode's, or under KeepMode the mode of the file it replaces.
//
// What it costs when this fails: a program written to be run cannot be, or is runnable by more
// than its caller asked.
func TestARunnableFileIs0700UnlessAModeIsGiven(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, row := range []struct {
		name string
		was  os.FileMode
		opts []Option
		want os.FileMode
	}{
		{"plain", 0, []Option{Runnable()}, 0o700},
		{"mode", 0, []Option{Mode(0o750), Runnable()}, 0o750},
		{"mode after", 0, []Option{Runnable(), Mode(0o500)}, 0o500},
		{"kept", 0o550, []Option{Runnable(), KeepMode()}, 0o550},
		{"kept, none there", 0, []Option{Runnable(), KeepMode()}, 0o700},
	} {
		path := filepath.Join(dir, row.name)
		if row.was != 0 {
			put0(t, path, "old")
			if err := os.Chmod(path, row.was); err != nil {
				t.Fatal(err)
			}
		}
		if err := File(path, []byte(prog), row.opts...); err != nil {
			t.Fatalf("%s: %v", row.name, err)
		}
		permIs(t, path, row.want)
	}
}

// Check on a Runnable file runs outside the fork lock, so it may start a process: here, the temp
// file itself, which no descriptor holds open for writing by then.
//
// What it costs when this fails: a Check that runs the program it checks waits on the fork lock
// its own write holds, for ever, and every fork in the process waits behind it.
func TestACheckOnARunnableFileMayRunIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "prog")
	written := make(chan error, 1)
	go func() {
		written <- File(path, []byte(prog), Runnable(), Check(func(*os.File) error {
			tmp, err := filepath.Glob(filepath.Join(dir, ".prog.*.tmp"))
			if err != nil || len(tmp) != 1 {
				return errors.Join(errors.New("no one temp file to run"), err)
			}
			return ranIt(exec.CommandContext(t.Context(), tmp[0]).Run()) //nolint:gosec // the temp file this test's own write made
		}))
	}()
	select {
	case err := <-written:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("a Check that starts a process is still waiting after 30 s")
	}
	holds(t, path, prog)
	if err := ranIt(exec.CommandContext(t.Context(), path).Run()); err != nil { //nolint:gosec // the file this test wrote
		t.Error(err)
	}
}

// forking reads prog once, but first starts a process and makes a pipe, as a reader that pipes
// from a program does: each takes the fork lock, the start its write side and, on macOS, the
// pipe its read side.
type forking struct {
	t    *testing.T
	read bool
}

func (r *forking) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	ran := make(chan error, 1)
	go func() { ran <- exec.CommandContext(r.t.Context(), "/bin/sh").Run() }() // reads /dev/null, exits 0
	if err := <-ran; err != nil {
		return 0, err
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return 0, err
	}
	if err := errors.Join(pr.Close(), pw.Close()); err != nil {
		return 0, err
	}
	return copy(p, prog), nil
}

// A FileFrom reader that starts a process and makes a pipe finishes under Runnable: the reader is
// read whole before the fork hold, so the fork it waits for is not waiting on the hold.
//
// What it costs when this fails: a program that writes a runnable file from a pipe or a
// download hangs for ever, now and then, whenever another goroutine starts a process as it
// reads, with every fork in the process behind it.
func TestAFileFromReaderThatForksFinishesUnderRunnable(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "prog")
	written := make(chan error, 1)
	go func() { written <- FileFrom(path, &forking{t: t}, Runnable()) }()
	select {
	case err := <-written:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("a FileFrom reader that starts a process is still waiting after 30 s")
	}
	holds(t, path, prog)
}

package write

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A Runnable write on Windows takes no fork lock: it finishes while a process start holds the
// lock's write side.
//
// What it costs when this fails: every Runnable write waits behind each process start in the
// process, for a race Windows does not have, since a child inherits only the handles made
// inheritable.
func TestOnWindowsARunnableWriteTakesNoForkLock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "prog.cmd")
	syscall.ForkLock.Lock()
	written := make(chan error, 1)
	go func() { written <- File(path, []byte("@exit /b 3\r\n"), Runnable()) }()
	select {
	case err := <-written:
		syscall.ForkLock.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		syscall.ForkLock.Unlock()
		t.Fatalf("a Runnable write waited on the fork lock 10 s, and then gave %v", <-written)
	}
	holds(t, path, "@exit /b 3\r\n")
	alone(t, dir, "prog.cmd")
}

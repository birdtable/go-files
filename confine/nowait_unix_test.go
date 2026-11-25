//go:build unix

package confine

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// opened is Open of name in root with flag, or a test error once ten seconds pass with Open still
// waiting; a pipe it waits on is opened from the other end at the test's end, so it returns.
func opened(t *testing.T, root *os.Root, name string, flag int) (*os.File, error) {
	t.Helper()
	return waited(t, "Open", filepath.Join(root.Name(), name), flag, func() (*os.File, error) { return Open(root, name, flag, 0) })
}

// waited is open's answer, or a test error naming call once ten seconds pass with open still
// waiting on path; a pipe at path it waits on is opened from the other end then, so it returns.
func waited(t *testing.T, call, path string, flag int, open func() (*os.File, error)) (*os.File, error) {
	t.Helper()
	type answer struct {
		f   *os.File
		err error
	}
	done := make(chan answer, 1)
	go func() {
		f, err := open()
		done <- answer{f, err}
	}()
	select {
	case a := <-done:
		return a.f, a.err
	case <-time.After(10 * time.Second):
		t.Errorf("%s(%q, %#x) is still waiting after ten seconds", call, path, flag)
		other := os.O_WRONLY
		if flag&(os.O_WRONLY|os.O_RDWR) != 0 {
			other = os.O_RDONLY
		}
		if f, err := os.OpenFile(path, other|unix.O_NONBLOCK, 0); err == nil {
			defer f.Close()
		}
		a := <-done
		return a.f, a.err
	}
}

// Open of a pipe never waits for its other end: to be read it opens at once, as a pipe the
// caller's check refuses, and to be written with no reader it is refused at once. A regular file
// opened the same way reads and writes as ever.
//
// What it costs when this fails: a pipe swapped in at a name after the caller looked at it hangs
// the open, and the program with it, for as long as the swapper likes.
func TestOpenNeverWaitsOnAPipe(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(dir, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	f, err := opened(t, root, "pipe", os.O_RDONLY)
	if err != nil {
		t.Fatalf("Open of a pipe to be read: %v, and want it open at once", err)
	}
	fi, err := f.Stat()
	if err != nil || fi.Mode().Type() != fs.ModeNamedPipe {
		t.Errorf("Open of a pipe gave %v (%v), and want the pipe, for the caller to refuse", fi, err)
	}
	f.Close()
	if f, err := opened(t, root, "pipe", os.O_WRONLY); err == nil {
		f.Close()
		t.Error("Open of a pipe with no reader, to be written, opened, and want it refused at once")
	}

	f, err = opened(t, root, "file", os.O_RDWR|os.O_APPEND)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(" new"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "file"))
	if err != nil || string(b) != "old new" {
		t.Errorf("the file opened with no wait holds %q (%v), and want %q", b, err, "old new")
	}
}

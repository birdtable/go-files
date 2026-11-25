//go:build unix

package confine_test

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/birdtable/go-files/confine"
)

// What it costs when this fails: a pipe a hostile process left in the repository holds the read
// until a writer comes, so a program reading its files hangs, or reads what the writer sends as the
// file's bytes.
func TestReadCappedRefusesAPipeWithoutWaiting(t *testing.T) {
	t.Parallel()
	in := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(in, "pipe"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := confine.ReadCapped(openRoot(t, in), "pipe", 64)
	if got != nil || !errors.Is(err, confine.ErrNotRegular) {
		t.Errorf("a pipe: %q, %v, and want nothing and ErrNotRegular", got, err)
	}
}

// What it costs when this fails: a socket in the repository is read as a file, or its refusal is
// said to be ErrNotRegular where the open itself refuses it, so a caller that branches on
// ErrNotRegular takes it for another failure unknowingly.
func TestReadCappedRefusesASocketAtTheOpen(t *testing.T) {
	t.Parallel()
	in, err := os.MkdirTemp("", "rc") // short, as a socket's path is bounded
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(in) })
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(fd) })
	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: filepath.Join(in, "sock")}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(filepath.Join(in, "sock")); err != nil || fi.Mode().Type() != os.ModeSocket {
		t.Fatalf("the socket is %v, %v", fi, err)
	}
	got, err := confine.ReadCapped(openRoot(t, in), "sock", 64)
	if got != nil || err == nil || errors.Is(err, confine.ErrNotRegular) {
		t.Errorf("a socket: %q, %v, and want nothing and the open's own refusal, not ErrNotRegular", got, err)
	}
}

//go:build unix

package lock

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// A hard link at the lock path is locked as the file it is, and left byte for byte, its mode and
// owner as they were: the lock never writes into its file.
//
// What it costs when this fails: whoever can write a folder on the way to a lock links a file of
// theirs there, and a program that takes the lock writes over it.
func TestAHardLinkAtTheLockPathIsNeverWrittenInto(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "precious")
	keep := []byte("keep\n")
	if err := os.WriteFile(target, keep, 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "app.lock")
	if err := os.Link(target, path); err != nil {
		t.Fatal(err)
	}
	l := take(t, path)
	if err := l.Note("mine"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, keep) {
		t.Errorf("the target holds %q, %v", got, err)
	}
	if fi, err := os.Lstat(target); err != nil || fi.Mode() != 0o644 {
		t.Errorf("the target is %v, %v", fi.Mode(), err)
	}
}

// A pipe at <name>.holder is read as a torn note at once, not waited on, with no writer and with a
// writer that holds it open and writes nothing: Holder, a declined Take and a Wait whose context has
// ended each say "another process" and return.
//
// What it costs when this fails: whoever can make a name in the lock's folder hangs every run that
// is told the lock is held, every Wait past its deadline, and every Holder.
func TestAPipeAtTheNoteIsNotWaitedOn(t *testing.T) {
	t.Parallel()
	path := tempLock(t)
	take(t, path)
	if err := syscall.Mkfifo(path+".holder", 0o600); err != nil {
		t.Fatal(err)
	}
	if h, note, err := Holder(path + ".holder"); err != nil || h != (Held{}) || note != nil {
		t.Errorf("Holder of a pipe with no writer: %#v, %q, %v", h, note, err)
	}
	w, err := os.OpenFile(path+".holder", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	if h, note, err := Holder(path + ".holder"); err != nil || h != (Held{}) || note != nil {
		t.Errorf("Holder of a pipe a writer holds: %#v, %q, %v", h, note, err)
	}
	var h Held
	if l, err := Take(path, path+".holder"); l != nil || !errors.As(err, &h) || h != (Held{}) {
		t.Errorf("a declined Take beside a pipe a writer holds: %v, %v", l, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if l, err := Wait(ctx, path, path+".holder"); l != nil || !IsHeld(err) || !errors.Is(err, context.Canceled) {
		t.Errorf("a Wait whose context ended, beside a pipe a writer holds: %v, %v", l, err)
	}
}

// A socket at <name>.holder reads as a torn note, not an error: the open of one fails before the
// plain-file check (ENXIO on Linux, EOPNOTSUPP on macOS). The folder is a short one, as a socket's
// path is held to about 104 bytes.
//
// What it costs when this fails: whoever can make a name in the lock's folder makes every Holder
// call fail.
func TestASocketAtTheNoteReadsAsATornNote(t *testing.T) {
	t.Parallel()
	dir, err := os.MkdirTemp("", "lk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "a.lock")
	take(t, path)
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(fd) })
	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: path + ".holder"}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(path + ".holder"); err != nil || fi.Mode().Type() != os.ModeSocket {
		t.Fatalf("the socket is %v, %v", fi, err)
	}
	if h, note, err := Holder(path + ".holder"); err != nil || h != (Held{}) || note != nil {
		t.Errorf("Holder of a socket: %#v, %q, %v, and want Held{} and no error", h, note, err)
	}
	var h Held
	if l, err := Take(path, path+".holder"); l != nil || !errors.As(err, &h) || h != (Held{}) {
		t.Errorf("a declined Take beside a socket: %v, %v", l, err)
	}
}

// A pipe or a socket at the lock path is refused, not locked, and never as Held.
//
// What it costs when this fails: a program takes a lock on a pipe another account made, which
// that account can hold open, or is told the lock is held when the lock file is not one.
func TestAPipeOrASocketAtTheLockPathIsRefused(t *testing.T) {
	t.Parallel()
	dir, err := os.MkdirTemp("", "lk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	pipe := filepath.Join(dir, "p.lock")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "s.lock")
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(fd) })
	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: sock}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{pipe, sock} {
		if l, err := Take(path, path+".holder"); l != nil || !errors.Is(err, errNotPlain) || IsHeld(err) {
			t.Errorf("Take of %s: %v, %v, and want it refused as not a plain file", filepath.Base(path), l, err)
		}
	}
}

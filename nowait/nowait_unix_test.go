//go:build unix

package nowait_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/birdtable/go-files/nowait"
)

// opened is Open's answer, or a test error once ten seconds pass with Open still waiting on path;
// a pipe at path it waits on is opened from the other end then, so it returns.
func opened(t *testing.T, path string, flag int) (*os.File, error) {
	t.Helper()
	type answer struct {
		f   *os.File
		err error
	}
	done := make(chan answer, 1)
	go func() {
		f, err := nowait.Open(path, flag, 0)
		done <- answer{f, err}
	}()
	select {
	case a := <-done:
		return a.f, a.err
	case <-time.After(10 * time.Second):
		t.Errorf("Open(%q, %#x) is still waiting after ten seconds", path, flag)
		other := os.O_WRONLY
		if flag&(os.O_WRONLY|os.O_RDWR) != 0 {
			other = os.O_RDONLY
		}
		if f, err := os.OpenFile(path, other|syscall.O_NONBLOCK, 0); err == nil {
			defer f.Close()
		}
		a := <-done
		return a.f, a.err
	}
}

// Open of a pipe, named by its absolute path, never waits for its other end: with no writer it
// opens at once to be read, as the pipe it is, and with no reader it is refused at once (ENXIO)
// to be written. A regular file opened the same way reads and writes as ever.
//
// What it costs when this fails: a pipe a user names, or one swapped in at a path the program
// opens, hangs the open, and the program with it, until something opens the other end.
func TestOpenNeverWaitsOnAPipe(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pipe := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	f, err := opened(t, pipe, os.O_RDONLY)
	if err != nil {
		t.Fatalf("Open of a pipe with no writer, to be read: %v, and want it open at once", err)
	}
	fi, err := f.Stat()
	if err != nil || fi.Mode().Type() != fs.ModeNamedPipe {
		t.Errorf("Open of a pipe gave %v (%v), and want the pipe, for the caller to refuse", fi, err)
	}
	f.Close()
	f, err = opened(t, pipe, os.O_WRONLY)
	if err == nil {
		f.Close()
		t.Error("Open of a pipe with no reader, to be written, opened, and want it refused at once")
	} else if !errors.Is(err, syscall.ENXIO) {
		t.Errorf("Open of a pipe with no reader, to be written: %v, and want ENXIO", err)
	}

	f, err = opened(t, file, os.O_RDWR|os.O_APPEND)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(" new"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(file)
	if err != nil || string(b) != "old new" {
		t.Errorf("the file opened with no wait holds %q (%v), and want %q", b, err, "old new")
	}
}

// Open follows a link, as its doc says and os.OpenFile does: a link at the last component and one
// at a folder on the way each open the file they lead to.
//
// What it costs when this fails: the doc and the call disagree, and a program that reads the doc
// as a promise of no link followed, or of one followed, is wrong either way.
func TestOpenFollowsALinkAsOsOpenFileDoes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "real"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real", "file"), []byte("held"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(dir, "folder")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("real", "file"), filepath.Join(dir, "last")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(dir, "last"), filepath.Join(dir, "folder", "file")} {
		f, err := nowait.Open(p, os.O_RDONLY, 0)
		if err != nil {
			t.Errorf("Open(%q) through a link: %v, and want the file it leads to", p, err)
			continue
		}
		b := make([]byte, 8)
		n, err := f.Read(b)
		f.Close()
		if err != nil || string(b[:n]) != "held" {
			t.Errorf("Open(%q) read %q (%v), and want %q", p, b[:n], err, "held")
		}
	}
}

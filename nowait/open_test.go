package nowait_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/nowait"
)

// Open takes a clean absolute path alone: a relative path, or one filepath.Clean would change, is
// refused with an *fs.PathError of op "open" matching fs.ErrInvalid, and nothing is opened or
// made. A clean absolute path opens as os.OpenFile opens it.
//
// What it costs when this fails: a relative path opens against whatever folder the process is
// in, and an unclean one names through a ".." a file the caller never named.
func TestOpenTakesACleanAbsolutePathOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sep := string(filepath.Separator)
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("held"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{
		"file", "." + sep + "file", "sub" + sep + ".." + sep + "file", ".", "",
		dir + sep + "sub" + sep + ".." + sep + "file",
		dir + sep + "." + sep + "file",
		dir + sep + "sub" + sep,
		dir + sep + sep + "file",
		dir + sep + "sub" + sep + ".." + sep + "made",
	} {
		flag := os.O_RDWR | os.O_CREATE // an absolute row may make a file, in dir alone
		if !filepath.IsAbs(p) {
			flag = os.O_RDONLY // a relative row names the test's own folder, so it makes nothing there
		}
		f, err := nowait.Open(p, flag, 0o600)
		if err == nil {
			f.Close()
			t.Errorf("Open(%q) opened, and want it refused", p)
			continue
		}
		pe, ok := errors.AsType[*fs.PathError](err)
		if !ok || pe.Op != "open" || pe.Path != p || !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Open(%q): %v, and want an *fs.PathError of op open on that path, matching fs.ErrInvalid", p, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "made")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a refused Open made %q (%v), and want nothing made", filepath.Join(dir, "made"), err)
	}

	f, err := nowait.Open(file, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Open(%q) of a clean absolute path: %v, and want it open", file, err)
	}
	defer f.Close()
	b := make([]byte, 8)
	n, err := f.Read(b)
	if err != nil || string(b[:n]) != "held" {
		t.Errorf("Open(%q) read %q (%v), and want %q", file, b[:n], err, "held")
	}
}

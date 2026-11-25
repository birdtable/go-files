//go:build linux || darwin || freebsd

package confine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/birdtable/go-files/confine"
)

// LinkFile makes a symbolic link at the root's top and below a folder, to a file outside the root
// and to one inside it; each says the target as given, and the file reads through it.
//
// What it costs when this fails: a program that links a file by this call gets a link that leads
// somewhere other than the file it named, or a copy that does not follow it.
func TestLinkFileMakesALinkToTheFile(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	outside := outsideFile(t)
	inside := filepath.Join(dir, "deep", "a.txt")
	for at, target := range map[string]string{"link": outside, filepath.Join("real", "link"): outside, "near": inside} {
		if err := confine.LinkFile(root, target, at); err != nil {
			t.Fatalf("LinkFile(%s, %s) = %v", target, at, err)
		}
		full := filepath.Join(dir, at)
		if info, err := os.Lstat(full); err != nil || info.Mode()&fs.ModeSymlink == 0 {
			t.Errorf("%s is %v, %v, want a symbolic link", at, info, err)
		}
		if got, err := os.Readlink(full); err != nil || got != target {
			t.Errorf("%s leads to %q, %v, want %q", at, got, err, target)
		}
		want := map[string]string{outside: "outside\n", inside: "a\n"}[target]
		if got, err := os.ReadFile(full); err != nil || string(got) != want {
			t.Errorf("%s holds %q, %v, want %q", at, got, err, want)
		}
	}
	unchanged(t, dir)
}

// LinkFile refuses a target that is there but not a regular file, a named pipe here, with an
// *os.LinkError matching fs.ErrInvalid, and makes nothing.
//
// What it costs when this fails: a link to a pipe or a device stands where the caller asked for a
// file, and a reader of it blocks or reads a device.
func TestLinkFileRefusesATargetThatIsNotARegularFile(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	pipe := filepath.Join(t.TempDir(), "pipe")
	if err := unix.Mkfifo(pipe, 0o600); err != nil {
		t.Fatal(err)
	}
	err := confine.LinkFile(root, pipe, "link")
	if _, ok := errors.AsType[*os.LinkError](err); !ok || !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("LinkFile(a pipe) = %v, want an *os.LinkError matching fs.ErrInvalid", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "link")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("link is there: %v", err)
	}
}

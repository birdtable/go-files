//go:build unix

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

// What it costs when this fails: a pipe swapped in at the name hangs the count on a writer that
// never comes, or a pipe or a device is answered a count as a file's.
func TestNamesRefusesAPipeAndADeviceAtOnce(t *testing.T) {
	t.Parallel()
	in := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(in, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err := confine.Names(openRoot(t, in), "pipe"); n != 0 || !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("a pipe: %d, %v, and want 0 and fs.ErrInvalid", n, err)
	}
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if n, err := confine.NamesFile(f); n != 0 || !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("%s: %d, %v, and want 0 and fs.ErrInvalid", os.DevNull, n, err)
	}
}

// What it costs when this fails: a file whose every name was removed while it was held is answered
// 1, one name, or an error, where its volume counts 0.
func TestNamesFileAnswersZeroForAFileWhoseNamesWereRemovedWhileHeld(t *testing.T) {
	t.Parallel()
	name := filepath.Join(t.TempDir(), "a")
	if err := os.WriteFile(name, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if n, err := confine.NamesFile(f); n != 0 || err != nil {
		t.Errorf("a held file of no name: %d, %v, and want 0 and no error", n, err)
	}
}

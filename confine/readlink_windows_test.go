package confine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// What it costs when this fails: a junction, which any account may make with no privilege, is
// read as no link, or passed through on the way to a name; or a reparse point of another tag is
// taken for a name that is not there.
func TestOnWindowsReadlinkReadsAJunctionAndRefusesAnotherReparsePoint(t *testing.T) {
	t.Parallel()
	tmp, root := readlinkTree(t)
	in := filepath.Join(tmp, "root")
	target := filepath.Join(tmp, "away")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := confine.LinkFolder(root, target, "j"); err != nil {
		t.Fatal(err)
	}
	if got, err := confine.Readlink(root, "j"); err != nil || got != target {
		t.Errorf("a junction made by LinkFolder: %q, %v, want %q", got, err, target)
	}
	if err := confine.LinkFolder(root, filepath.Join(in, "sub"), filepath.Join("sub", "jj")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(target, "x"))
	if err := confine.PlantOtherReparse(filepath.Join(in, "r")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{filepath.Join("j", "x"), filepath.Join("sub", "jj", "j"), "r"} {
		got, err := confine.Readlink(root, name)
		if pe, ok := errors.AsType[*fs.PathError](err); !ok || !errors.Is(err, confine.ErrOutside) || errors.Is(err, fs.ErrNotExist) || got != "" {
			t.Errorf("Readlink(%q) = %q, %v (%v), want ErrOutside and never fs.ErrNotExist", name, got, err, pe)
		}
	}
}

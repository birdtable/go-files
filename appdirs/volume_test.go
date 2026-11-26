//go:build darwin || linux || windows

package appdirs_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/appdirs"
)

// The temp folder, a file in it and a link to it are on a local disk volume on every runner, and
// Volume answers each Local, with the file system's name and no error.
//
// What it costs when this fails: a program that refuses a volume Volume does not answer Local
// refuses to start on the machine's own disk.
func TestVolumeAnswersTheTempFolderLocal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	paths := []string{dir, file}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Logf("no link on this runner: %v", err)
	} else {
		paths = append(paths, link)
	}
	for _, p := range paths {
		got, err := appdirs.Volume(p)
		t.Logf("Volume(%s) = %+v, %v", p, got, err)
		if err != nil || got.Kind != appdirs.Local || !got.Local() || got.Type == "" {
			t.Errorf("Volume(%s) = %+v, %v, want Local with its file system's name", p, got, err)
		}
	}
}

// A path that is not there is an *fs.PathError naming it, matching fs.ErrNotExist, with the zero
// VolumeInfo, which is not local.
//
// What it costs when this fails: a folder not made yet is answered as the volume of a folder above
// it, or an error comes with a VolumeInfo that reads as local.
func TestVolumeRefusesAPathThatIsNotThere(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "missing", "data")
	got, err := appdirs.Volume(missing)
	if pe, ok := errors.AsType[*fs.PathError](err); !ok || pe.Path != missing || !errors.Is(err, fs.ErrNotExist) || got != (appdirs.VolumeInfo{}) || got.Local() {
		t.Errorf("Volume(a missing path) = %+v, %v, want the zero VolumeInfo and an *fs.PathError naming it matching fs.ErrNotExist", got, err)
	}
}

//go:build !linux && !darwin && !freebsd && !windows

package confine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// LinkFolder on a system with no symlinkat from a folder held open refuses with
// errors.ErrUnsupported, naming at, and makes nothing.
//
// What it costs when this fails: a link made by name here would follow a link swapped into at's
// folder, and land outside the root.
func TestLinkFolderOnThisSystemIsRefused(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	target := t.TempDir()
	err := confine.LinkFolder(root, target, "link")
	if pe, ok := errors.AsType[*fs.PathError](err); !ok || !errors.Is(err, errors.ErrUnsupported) || pe.Path != "link" {
		t.Errorf("LinkFolder = %v, want errors.ErrUnsupported naming link", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "link")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("link is there: %v", err)
	}
	unchanged(t, dir)
}

// LinkFile on a system with no symlinkat from a folder held open refuses with ErrNoFileLink,
// which matches errors.ErrUnsupported, naming at, and makes nothing.
//
// What it costs when this fails: a link made by name here would follow a link swapped into at's
// folder, and land outside the root.
func TestLinkFileOnThisSystemIsRefused(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	err := confine.LinkFile(root, filepath.Join(dir, "beside.txt"), "link")
	if pe, ok := errors.AsType[*fs.PathError](err); !ok || !errors.Is(err, confine.ErrNoFileLink) || !errors.Is(err, errors.ErrUnsupported) || pe.Path != "link" {
		t.Errorf("LinkFile = %v, want ErrNoFileLink and errors.ErrUnsupported naming link", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "link")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("link is there: %v", err)
	}
	unchanged(t, dir)
}

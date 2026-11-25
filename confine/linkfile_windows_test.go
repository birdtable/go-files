package confine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/birdtable/go-files/confine"
)

// LinkFile makes a symbolic link to the file, which says the target as given and reads it, where
// this account may make one; where it may not, as without Developer Mode, it refuses with
// ErrNoFileLink, matching errors.ErrUnsupported, and nothing is at the name. Never a hard link or
// a copy: what is there is a symbolic link or nothing.
//
// What it costs when this fails: a program that links a file gets a copy that does not follow the
// file, a hard link that a later write through it changes in place, or a file left at the name.
func TestOnWindowsLinkFileMakesASymbolicLinkOrRefuses(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	target := outsideFile(t)
	for _, at := range []string{"link", filepath.Join("real", "link")} {
		full := filepath.Join(dir, at)
		err := confine.LinkFile(root, target, at)
		if errors.Is(err, confine.ErrNoFileLink) {
			t.Logf("this account may not make a symbolic link: %v", err)
			if !errors.Is(err, errors.ErrUnsupported) {
				t.Errorf("LinkFile(%s) = %v, want it to match errors.ErrUnsupported", at, err)
			}
			if _, err := os.Lstat(full); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("after the refusal, %s is there: %v", at, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("LinkFile(%s, %s) = %v", target, at, err)
		}
		t.Logf("this account made %s a symbolic link", at)
		if info, err := os.Lstat(full); err != nil || info.Mode()&fs.ModeSymlink == 0 {
			t.Errorf("%s is %v, %v, want a symbolic link", at, info, err)
		}
		if got, err := os.Readlink(full); err != nil || got != target {
			t.Errorf("%s leads to %q, %v, want %q", at, got, err, target)
		}
		if got, err := os.ReadFile(full); err != nil || string(got) != "outside\n" {
			t.Errorf("%s holds %q, %v, want the target's", at, got, err)
		}
	}
	unchanged(t, dir)
}

// When the system refuses the symbolic link for want of the privilege, the file LinkFile made for
// it, there when the set runs, is removed, and the refusal is ErrNoFileLink.
//
// What it costs when this fails: a refused link leaves an empty file at its name, which a program
// reads as the file it linked, or the refusal is not one a caller can tell from any other.
func TestOnWindowsAFileLinkRefusedForThePrivilegeIsRemoved(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	target := outsideFile(t)
	for _, at := range []string{"link", filepath.Join("deep", "link")} {
		made := false
		err := confine.LinkFileSet(root, target, at, func() error {
			info, err := os.Lstat(filepath.Join(dir, at))
			made = err == nil && info.Mode().IsRegular()
			return windows.ERROR_PRIVILEGE_NOT_HELD
		})
		if !errors.Is(err, confine.ErrNoFileLink) || !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("LinkFile(%s) = %v, want ErrNoFileLink matching errors.ErrUnsupported", at, err)
		}
		if !made {
			t.Errorf("no file was at %s when the set ran", at)
		}
		if _, err := os.Lstat(filepath.Join(dir, at)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s is left there: %v", at, err)
		}
	}
	unchanged(t, dir)
}

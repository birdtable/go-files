//go:build linux || darwin || freebsd || windows

package confine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// outsideFile makes a file outside any root a test opens, holding "outside\n", as a link's
// target.
func outsideFile(t *testing.T) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(target, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return target
}

// LinkFile refuses, matching ErrOutside, a name that is not clean and below the root, and one
// through a link at a component of its folder, naming that link; a name already there, a link
// among them, is fs.ErrExist and is left as it was. Nothing is made.
//
// What it costs when this fails: a name from a flag or a file makes a link outside the folder a
// program keeps, or through a link a repository committed; or a name there is replaced.
func TestLinkFileRefusesANameOutsideTheRootOrThroughALink(t *testing.T) {
	t.Parallel()
	root, dir := folder(t)
	target := outsideFile(t)
	sep := string(filepath.Separator)
	for _, at := range []string{"", ".", "..", filepath.Join("..", "out"), filepath.Join(dir, "out"), "a" + sep + ".." + sep + "out", "out" + sep} {
		if err := confine.LinkFile(root, target, at); !errors.Is(err, confine.ErrOutside) {
			t.Errorf("LinkFile(%q) = %v, want ErrOutside", at, err)
		}
	}
	for at, link := range map[string]string{filepath.Join("state", "link"): "state", filepath.Join("deep", "via", "link"): "deep/via"} {
		refusedAt(t, "LinkFile("+at+")", confine.LinkFile(root, target, at), link)
	}
	for _, at := range []string{"state", "dangling", "beside.txt", "real"} {
		before, err := os.Lstat(filepath.Join(dir, at))
		if err != nil {
			t.Fatal(err)
		}
		err = confine.LinkFile(root, target, at)
		if pe, ok := errors.AsType[*fs.PathError](err); !ok || !errors.Is(err, fs.ErrExist) || pe.Path != at {
			t.Errorf("LinkFile(%s) = %v, want fs.ErrExist naming it", at, err)
		}
		if after, err := os.Lstat(filepath.Join(dir, at)); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
			t.Errorf("%s is %v, %v, want it as it was", at, after, err)
		}
	}
	unchanged(t, dir)
	for _, gone := range []string{filepath.Join(filepath.Dir(dir), "out"), filepath.Join(dir, "out"), filepath.Join(dir, "real", "link")} {
		if _, err := os.Lstat(gone); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s is there: %v", gone, err)
		}
	}
}

// LinkFile refuses a target that is relative, unclean, a folder or missing, with an
// *os.LinkError matching fs.ErrInvalid, or fs.ErrNotExist for a missing one, and makes nothing.
//
// What it costs when this fails: a relative target is read from the link's own folder, a
// different place than the caller meant, and a link to a folder is a second way into it that the
// caller asked to be a file.
func TestLinkFileRefusesATargetThatIsNotAnAbsoluteFile(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	sep := string(filepath.Separator)
	refused := map[string]error{
		"beside.txt": fs.ErrInvalid,
		dir + sep + "deep" + sep + ".." + sep + "beside.txt": fs.ErrInvalid,
		dir + sep + "beside.txt" + sep:                       fs.ErrInvalid,
		filepath.Join(dir, "real"):                           fs.ErrInvalid,
		dir:                                                  fs.ErrInvalid,
		filepath.Join(dir, "gone"):                           fs.ErrNotExist,
	}
	for target, want := range refused {
		err := confine.LinkFile(root, target, "link")
		if _, ok := errors.AsType[*os.LinkError](err); !ok || !errors.Is(err, want) {
			t.Errorf("LinkFile(%q) = %v, want an *os.LinkError matching %v", target, err, want)
		}
		if _, err := os.Lstat(filepath.Join(dir, "link")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("after LinkFile(%q), link is there: %v", target, err)
		}
	}
	unchanged(t, dir)
}

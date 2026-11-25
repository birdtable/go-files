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

// outsideFolder makes a folder outside any root a test opens, holding inside.txt, as a link's
// target.
func outsideFolder(t *testing.T) string {
	t.Helper()
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "inside.txt"), []byte("inside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return target
}

// empty fails the test unless nothing but inside.txt is in target: nothing was made through a
// link into it.
func empty(t *testing.T, target string) {
	t.Helper()
	got, err := os.ReadDir(target)
	if err != nil || len(got) != 1 || got[0].Name() != "inside.txt" {
		t.Errorf("the target holds %v, %v, want inside.txt alone", got, err)
	}
}

// LinkFolder makes a link at the root's top and below a folder, to a folder outside the root and
// to one inside it; each is a link, not a folder, it says the target as given, and the target's
// file reads through it.
//
// What it costs when this fails: a program that links a folder by this call gets a link that
// leads somewhere other than the folder it named, or a plain folder that leads nowhere.
func TestLinkFolderMakesALinkToTheFolder(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	outside := outsideFolder(t)
	inside := filepath.Join(dir, "deep")
	made := map[string]string{"link": outside, filepath.Join("real", "link"): outside, "near": inside}
	for at, target := range made {
		if err := confine.LinkFolder(root, target, at); err != nil {
			t.Fatalf("LinkFolder(%s, %s) = %v", target, at, err)
		}
		full := filepath.Join(dir, at)
		if info, err := os.Lstat(full); err != nil || info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) == 0 {
			t.Errorf("%s is %v, %v, want a link", at, info, err)
		}
		if got, err := os.Readlink(full); err != nil || got != target {
			t.Errorf("%s leads to %q, %v, want %q", at, got, err, target)
		}
	}
	for at, want := range map[string]string{"link": "inside\n", filepath.Join("real", "link"): "inside\n", "near": "a\n"} {
		file := "inside.txt"
		if at == "near" {
			file = "a.txt"
		}
		if got, err := os.ReadFile(filepath.Join(dir, at, file)); err != nil || string(got) != want {
			t.Errorf("%s through %s holds %q, %v, want %q", file, at, got, err, want)
		}
	}
	unchanged(t, dir)
	empty(t, outside)
}

// LinkFolder refuses, matching ErrOutside, a name that is not clean and below the root, and one
// through a link at a component of its folder, naming that link; a name already there, a link
// among them, is fs.ErrExist and is left as it was. Nothing is made, in the root, beside it, or
// where a link leads.
//
// What it costs when this fails: a name from a flag or a file makes a link outside the folder a
// program keeps, or through a link a repository committed, so a later write through the new link
// lands where the attacker chose; or a name there is replaced.
func TestLinkFolderRefusesANameOutsideTheRootOrThroughALink(t *testing.T) {
	t.Parallel()
	root, dir := folder(t)
	target := outsideFolder(t)
	sep := string(filepath.Separator)
	outside := []string{"", ".", "..", filepath.Join("..", "out"), filepath.Join(dir, "out"),
		"a" + sep + ".." + sep + "out", "a" + sep + sep + "out", "." + sep + "out", "out" + sep}
	for _, at := range outside {
		if err := confine.LinkFolder(root, target, at); !errors.Is(err, confine.ErrOutside) {
			t.Errorf("LinkFolder(%q) = %v, want ErrOutside", at, err)
		}
	}
	through := map[string]string{
		filepath.Join("state", "link"):         "state",
		filepath.Join("deep", "via", "link"):   "deep/via",
		filepath.Join("history.jsonl", "link"): "history.jsonl",
	}
	for at, link := range through {
		refusedAt(t, "LinkFolder("+at+")", confine.LinkFolder(root, target, at), link)
	}
	for _, at := range []string{"state", "dangling", "beside.txt", "real"} {
		before, err := os.Lstat(filepath.Join(dir, at))
		if err != nil {
			t.Fatal(err)
		}
		err = confine.LinkFolder(root, target, at)
		if pe, ok := errors.AsType[*fs.PathError](err); !ok || !errors.Is(err, fs.ErrExist) || pe.Path != at {
			t.Errorf("LinkFolder(%s) = %v, want fs.ErrExist naming it", at, err)
		}
		if after, err := os.Lstat(filepath.Join(dir, at)); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
			t.Errorf("%s is %v, %v, want it as it was", at, after, err)
		}
	}
	unchanged(t, dir)
	empty(t, target)
	for _, gone := range []string{filepath.Join(filepath.Dir(dir), "out"), filepath.Join(dir, "out"), filepath.Join(dir, "real", "link")} {
		if _, err := os.Lstat(gone); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s is there: %v", gone, err)
		}
	}
}

// LinkFolder refuses a target that is relative, unclean, a file or missing, with an
// *os.LinkError matching fs.ErrInvalid, or fs.ErrNotExist for a missing one, and makes nothing.
//
// What it costs when this fails: a relative target is read from the link's own folder, a
// different place than the caller meant, and a link to a file or to nothing is a folder link that
// leads nowhere a program can list.
func TestLinkFolderRefusesATargetThatIsNotAnAbsoluteFolder(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	sep := string(filepath.Separator)
	refused := map[string]error{
		"real": fs.ErrInvalid,
		dir + sep + "deep" + sep + ".." + sep + "real": fs.ErrInvalid,
		dir + sep + "real" + sep:                       fs.ErrInvalid,
		filepath.Join(dir, "beside.txt"):               fs.ErrInvalid,
		filepath.Join(dir, "gone"):                     fs.ErrNotExist,
	}
	for target, want := range refused {
		err := confine.LinkFolder(root, target, "link")
		if _, ok := errors.AsType[*os.LinkError](err); !ok || !errors.Is(err, want) {
			t.Errorf("LinkFolder(%q) = %v, want an *os.LinkError matching %v", target, err, want)
		}
		if _, err := os.Lstat(filepath.Join(dir, "link")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("after LinkFolder(%q), link is there: %v", target, err)
		}
	}
	unchanged(t, dir)
}

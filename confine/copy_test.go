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

// program is the name of a file every system runs as written 0o755: Windows by its extension, the
// others by its exec bit.
const program = "run.exe"

// srcTree makes a folder holding the files named, each with its own path as its content, a program
// among them 0o755 by its mode, and an empty folder, and answers it.
func srcTree(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		p := filepath.Join(dir, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// rootOf opens dir as a root closed at the test's end.
func rootOf(t *testing.T, dir string) *os.Root {
	t.Helper()
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

// holdsPaths fails the test unless each name in dir holds its own path, read with no link followed.
func holdsPaths(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		p := filepath.Join(dir, filepath.FromSlash(n))
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() {
			t.Errorf("%s is %v, %v, and want a file", n, info, err)
			continue
		}
		if got, err := os.ReadFile(p); err != nil || string(got) != n {
			t.Errorf("%s holds %q, %v, and want %q", n, got, err, n)
		}
	}
}

// copyRefused fails the test unless err matches want and is an *fs.PathError naming name.
func copyRefused(t *testing.T, err, want error, name string) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("CopyTree = %v, and want it to match %v", err, want)
	}
	if pe, ok := errors.AsType[*fs.PathError](err); !ok || pe.Path != name {
		t.Errorf("CopyTree = %v, and want an *fs.PathError naming %s", err, name)
	}
}

// CopyTree copies each file with its bytes, each folder, the empty one too, and keeps a program
// runnable and a plain file not, as IsRunnable answers them in the copy; src is left as it was.
//
// What it costs when this fails: a fixture or a recorded tree is copied short, or with a program
// that no longer runs, or a script that suddenly does.
func TestCopyTreeCopiesEachFolderAndFileAndKeepsAProgramRunnable(t *testing.T) {
	t.Parallel()
	names := []string{"a.txt", "sub/b.txt", "sub/deep/c.txt", "bin/" + program}
	from := srcTree(t, names...)
	if err := os.Mkdir(filepath.Join(from, "none"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(from, "a.txt"), 0o644); err != nil {
		t.Fatal(err)
	}
	to := t.TempDir()
	dst := rootOf(t, to)
	if err := confine.CopyTree(dst, rootOf(t, from)); err != nil {
		t.Fatal(err)
	}
	holdsPaths(t, to, names...)
	holdsPaths(t, from, names...)
	if info, err := os.Lstat(filepath.Join(to, "none")); err != nil || !info.IsDir() {
		t.Errorf("none is %v, %v, and want a folder", info, err)
	}
	for name, want := range map[string]bool{"bin/" + program: true, "a.txt": false} {
		if got, err := confine.IsRunnable(dst, filepath.FromSlash(name)); err != nil || got != want {
			t.Errorf("IsRunnable(%s) in the copy = %v, %v, and want %v", name, got, err, want)
		}
	}
}

// CopyTree makes each link of src that leads inside it again, to the same name in the copy, by a
// target relative to the link's own folder, or on Windows an absolute one under dst: a folder link
// (a junction on Windows), an absolute or a relative one, one to a link, one to src itself and one
// to a file. Each is a link in the copy, and reads the copy's file, never src's.
//
// What it costs when this fails: a copied tree's links still lead into the source, so a write
// through the copy changes the source, or a link is copied as a folder of its own.
func TestCopyTreeMakesEachLinkInsideAgainToTheSameNameOfTheCopy(t *testing.T) {
	t.Parallel()
	from := srcTree(t, "real/f.txt", "sub/g.txt")
	src := rootOf(t, from)
	if err := confine.LinkFolder(src, filepath.Join(from, "real"), "abs"); err != nil {
		t.Fatal(err)
	}
	confine.PlantLink(t, filepath.Join("..", "real"), filepath.Join(from, "sub", "up"))
	confine.PlantLink(t, "abs", filepath.Join(from, "chain"))
	confine.PlantLink(t, filepath.Join(from, "real", "f.txt"), filepath.Join(from, "file"))
	if err := confine.LinkFolder(src, from, "self"); err != nil {
		t.Fatal(err)
	}
	to := t.TempDir()
	if err := confine.CopyTree(rootOf(t, to), src); err != nil {
		t.Fatal(err)
	}
	holdsPaths(t, to, "real/f.txt", "sub/g.txt")
	links := map[string]string{
		"abs":                      filepath.Join(to, "real"),
		filepath.Join("sub", "up"): filepath.Join(to, "real"),
		"chain":                    filepath.Join(to, "real"),
		"file":                     filepath.Join(to, "real", "f.txt"),
		"self":                     to,
	}
	for at, want := range links {
		p := filepath.Join(to, at)
		if info, err := os.Lstat(p); err != nil || info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) == 0 {
			t.Errorf("%s in the copy is %v, %v, and want a link", at, info, err)
		}
		got, err := os.Readlink(p)
		if err != nil || filepath.IsAbs(got) != confine.AbsoluteLinks {
			t.Errorf("%s in the copy holds %q, %v, and want it relative, or absolute on Windows", at, got, err)
		}
		if !filepath.IsAbs(got) {
			got = filepath.Join(filepath.Dir(p), got)
		}
		if got != want {
			t.Errorf("%s in the copy leads to %q, and want %q", at, got, want)
		}
	}
	if err := os.WriteFile(filepath.Join(to, "real", "f.txt"), []byte("copy"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, through := range []string{filepath.Join("abs", "f.txt"), filepath.Join("sub", "up", "f.txt"), filepath.Join("chain", "f.txt"), "file", filepath.Join("self", "real", "f.txt")} {
		if got, err := os.ReadFile(filepath.Join(to, through)); err != nil || string(got) != "copy" {
			t.Errorf("%s in the copy reads %q, %v, and want the copy's file", through, got, err)
		}
	}
	holdsPaths(t, from, "real/f.txt")
}

// CopyTree refuses a link that leads outside src, absolute, by a relative .., or through another
// link of the tree, matching ErrOutside, and one that leads to nothing, matching fs.ErrNotExist,
// each naming the link. No link is made in the copy, and nothing is made where one leads.
//
// What it costs when this fails: a tree from a less-trusted source carries a link out into the
// copy, so a program that writes through the copy writes where the source chose.
func TestCopyTreeRefusesALinkThatLeadsOutsideOrToNothing(t *testing.T) {
	t.Parallel()
	outside := outsideFolder(t)
	for name, c := range map[string]struct {
		plant func(t *testing.T, from string)
		at    string
		want  error
	}{
		"absolute": {func(t *testing.T, from string) {
			t.Helper()
			if err := confine.LinkFolder(rootOf(t, from), outside, "out"); err != nil {
				t.Fatal(err)
			}
		}, "out", confine.ErrOutside},
		"dotdot": {func(t *testing.T, from string) {
			t.Helper()
			up, err := filepath.Rel(filepath.Join(from, "sub"), outside)
			if err != nil {
				t.Fatal(err)
			}
			confine.PlantLink(t, up, filepath.Join(from, "sub", "out"))
		}, "sub/out", confine.ErrOutside},
		"through another": {func(t *testing.T, from string) {
			t.Helper()
			confine.PlantLink(t, filepath.Join("z", "inside.txt"), filepath.Join(from, "a"))
			if err := confine.LinkFolder(rootOf(t, from), outside, "z"); err != nil {
				t.Fatal(err)
			}
		}, "a", confine.ErrOutside},
		"to nothing": {func(t *testing.T, from string) {
			t.Helper()
			confine.PlantLink(t, filepath.Join(from, "missing"), filepath.Join(from, "gone"))
		}, "gone", fs.ErrNotExist},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			from := srcTree(t, "sub/b.txt")
			c.plant(t, from)
			to := t.TempDir()
			copyRefused(t, confine.CopyTree(rootOf(t, to), rootOf(t, from)), c.want, c.at)
			for _, at := range []string{"a", "gone", "out", filepath.Join("sub", "out"), "z"} {
				if _, err := os.Lstat(filepath.Join(to, at)); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("%s is in the copy (%v), and want no link made", at, err)
				}
			}
			empty(t, outside)
		})
	}
}

// CopyTree refuses a name already in dst, a file at a file's name, a folder at a folder's and a
// file at a folder's, matching fs.ErrExist and naming it, and leaves what is there as it was.
//
// What it costs when this fails: a copy into a folder that holds anything replaces a file there,
// or writes into a folder another made, with that folder's mode.
func TestCopyTreeRefusesANameAlreadyInDstAndLeavesIt(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ there, at string }{
		"a file at a file":     {"a.txt", "a.txt"},
		"a folder at a folder": {"sub/theirs", "sub"},
		"a file at a folder":   {"sub", "sub"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			from := srcTree(t, "a.txt", "sub/b.txt")
			to := srcTree(t, c.there)
			copyRefused(t, confine.CopyTree(rootOf(t, to), rootOf(t, from)), fs.ErrExist, c.at)
			holdsPaths(t, to, c.there)
		})
	}
}

// CopyTree refuses, before anything is made, a root whose Name is not absolute or not clean,
// matching fs.ErrInvalid, and src and dst one folder, matching ErrOutside; dst met in the walk of
// src is refused, matching ErrOutside and naming it, and never copied into itself.
//
// What it costs when this fails: a link of the copy names its target by a relative or unclean
// path that leads elsewhere, or a copy into a folder of its own source never ends.
func TestCopyTreeRefusesARootItCannotNameAndATreeIntoItself(t *testing.T) {
	t.Parallel()
	t.Run("relative", func(t *testing.T) {
		t.Parallel()
		to := t.TempDir()
		copyRefused(t, confine.CopyTree(rootOf(t, to), rootOf(t, "testdata")), fs.ErrInvalid, "testdata")
		if names, err := os.ReadDir(to); err != nil || len(names) != 0 {
			t.Errorf("dst holds %v, %v, and want nothing made", names, err)
		}
	})
	t.Run("unclean", func(t *testing.T) {
		t.Parallel()
		from := srcTree(t, "a.txt")
		unclean := from + string(filepath.Separator) + "."
		copyRefused(t, confine.CopyTree(rootOf(t, unclean), rootOf(t, t.TempDir())), fs.ErrInvalid, unclean)
		holdsPaths(t, from, "a.txt")
	})
	t.Run("one folder", func(t *testing.T) {
		t.Parallel()
		from := t.TempDir()
		copyRefused(t, confine.CopyTree(rootOf(t, from), rootOf(t, from)), confine.ErrOutside, from)
	})
	t.Run("dst inside src", func(t *testing.T) {
		t.Parallel()
		from := srcTree(t, "a/b.txt")
		to := filepath.Join(from, "a", "out")
		if err := os.Mkdir(to, 0o700); err != nil {
			t.Fatal(err)
		}
		copyRefused(t, confine.CopyTree(rootOf(t, to), rootOf(t, from)), confine.ErrOutside, "a/out")
		if _, err := os.Lstat(filepath.Join(to, "a", "out")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("a/out/a/out is there (%v), and want the copy stopped at dst", err)
		}
	})
}

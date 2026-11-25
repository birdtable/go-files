package confine

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// MkdirShared makes each missing folder of a name and answers the last as a root, in which a file
// made lands at the name, as Mkdir does; MkdirPrivate does the same.
//
// What it costs when this fails: a program that makes a shared folder or a private one by name
// writes its files somewhere else.
func TestMkdirSharedAndMkdirPrivateAnswerTheLastFolder(t *testing.T) {
	t.Parallel()
	for what, mk := range map[string]func(*os.Root, string) (*os.Root, error){"MkdirShared": MkdirShared, "MkdirPrivate": MkdirPrivate} {
		dir := t.TempDir()
		root := rootAt(t, dir)
		r, err := mk(root, filepath.Join("a", "b"))
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		f, err := r.Create("f")
		if err == nil {
			err = f.Close()
		}
		if err := errors.Join(err, r.Close()); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(filepath.Join(dir, "a", "b", "f")); err != nil {
			t.Errorf("%s: a file made in the answered root is not at a/b/f: %v", what, err)
		}
		again, err := mk(root, filepath.Join("a", "b"))
		if err != nil {
			t.Errorf("%s: a name whose folders are all there: %v", what, err)
		} else {
			again.Close()
		}
	}
}

// MkdirShared refuses what Mkdir refuses, each with ErrOutside: a name that is not clean, a link
// or a file at a component, and a folder holding something, or a link, swapped in for one it made,
// and nothing is made through the link or done to what was swapped in.
//
// What it costs when this fails: a folder meant for other accounts to read is made through a link
// another account planted, beside its secrets, or a folder of secrets renamed in at the name is
// answered as the shared folder and written into.
func TestMkdirSharedRefusesWhatMkdirRefuses(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, d := range []string{"private", "beside"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, d, "x"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	plantLink(t, "private", filepath.Join(dir, "link"))
	root := rootAt(t, dir)
	for _, name := range []string{filepath.Join("..", "up"), "a" + string(filepath.Separator) + ".." + string(filepath.Separator) + "b", filepath.Join("link", "new"), filepath.Join("file", "new")} {
		r, err := MkdirShared(root, name)
		if err == nil {
			r.Close()
		}
		if !errors.Is(err, ErrOutside) {
			t.Errorf("MkdirShared %s: %v, and want ErrOutside", name, err)
		}
	}
	if names := namesIn(filepath.Join(dir, "private")); !slices.Equal(names, []string{"x"}) {
		t.Errorf("private, behind the link, holds %q, and want only its own x", names)
	}
	for what, link := range map[string]bool{"a folder holding a file renamed in": false, "a link": true} {
		at := "renamed"
		if link {
			at = "linked"
		}
		made := filepath.Join(dir, at)
		done := false
		r, err := mkdirAs(root, filepath.Join(at, "deeper"), 0o755, inherits, func(step string) {
			if step != stepMadeFolder || done {
				return
			}
			done = true
			if err := os.Remove(made); err != nil {
				t.Fatal(err)
			}
			if link {
				plantLink(t, "private", made)
				return
			}
			if err := os.Rename(filepath.Join(dir, "beside"), made); err != nil {
				t.Fatal(err)
			}
		}, false)
		if err == nil {
			r.Close()
		}
		if !done || !errors.Is(err, ErrOutside) {
			t.Errorf("%s swapped in for a folder MkdirShared made (reached %v): %v, and want ErrOutside", what, done, err)
		}
		held := made
		if link {
			held = filepath.Join(dir, "private")
		}
		if names := namesIn(held); !slices.Equal(names, []string{"x"}) {
			t.Errorf("%s swapped in: %s holds %q, and want only its own x", what, held, names)
		}
	}
}

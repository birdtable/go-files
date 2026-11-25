package confine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// removeFolder is a folder holding a file, an empty folder, a full one, a tree three deep with a
// link out of it to real, real itself, a link to real beside it, and a folder beside holding
// keep.txt, opened as a root.
func removeFolder(t *testing.T) (*os.Root, string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"f.txt":                      "f",
		"full/a.txt":                 "a",
		"tree/a.txt":                 "a",
		"tree/sub/b.txt":             "b",
		"tree/sub/deeper/c.txt":      "c",
		"real/HEAD":                  "head",
		"beside/keep.txt":            "keep",
		"beside/sub/keep.txt":        "keep",
		"beside/sub/deeper/keep.txt": "keep",
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		must(t, os.MkdirAll(filepath.Dir(path), 0o755))
		must(t, os.WriteFile(path, []byte(body), 0o644))
	}
	must(t, os.Mkdir(filepath.Join(dir, "empty"), 0o755))
	plantLink(t, "real", filepath.Join(dir, "link"))
	plantLink(t, filepath.Join("..", "real"), filepath.Join(dir, "tree", "out"))
	return rootAt(t, dir), dir
}

// gone fails the test unless nothing is at name in dir.
func gone(t *testing.T, dir, name string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(name))); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s is still there (%v), and want it removed", name, err)
	}
}

// Remove removes a file, an empty folder, a file three folders down, and a link at the last
// component as the link, leaving what it leads to. A missing name matches fs.ErrNotExist, and a
// full folder is refused with nothing in it removed.
//
// What it costs when this fails: a program removing a link to its state removes the state, or one
// told a name is gone takes a full folder's refusal for success.
func TestRemoveRemovesAFileALinkAndAnEmptyFolder(t *testing.T) {
	t.Parallel()
	root, dir := removeFolder(t)
	for _, name := range []string{"f.txt", "empty", "link", filepath.Join("tree", "sub", "deeper", "c.txt")} {
		if err := Remove(root, name); err != nil {
			t.Errorf("Remove(%s): %v", name, err)
		}
		gone(t, dir, name)
	}
	holds(t, dir, filepath.Join("real", "HEAD"), "head")
	if err := Remove(root, "missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Remove(missing): %v, and want fs.ErrNotExist", err)
	}
	if _, ok := errors.AsType[*fs.PathError](Remove(root, "missing")); !ok {
		t.Error("Remove(missing) is not an *fs.PathError")
	}
	if err := Remove(root, "full"); err == nil {
		t.Error("Remove(full) removed a folder that holds a file")
	}
	holds(t, dir, filepath.Join("full", "a.txt"), "a")
}

// RemoveAll removes a tree, and the link in it as the link: the folder it leads to keeps its file.
// A link at the name is removed as the link, and a missing name, or one below a missing folder, is
// nil.
//
// What it costs when this fails: a program clearing its cache follows a link planted in it and
// removes whatever the link leads to.
func TestRemoveAllRemovesATreeAndNotWhatItsLinksLeadTo(t *testing.T) {
	t.Parallel()
	root, dir := removeFolder(t)
	for _, name := range []string{"tree", "link", "f.txt", "missing", filepath.Join("missing", "deeper")} {
		if err := RemoveAll(root, name); err != nil {
			t.Errorf("RemoveAll(%s): %v", name, err)
		}
		gone(t, dir, name)
	}
	holds(t, dir, filepath.Join("real", "HEAD"), "head")
}

// A name that is not clean and local, or is the root itself, is refused by both with ErrOutside,
// and nothing is removed.
//
// What it costs when this fails: a name built from input, ../x or an absolute path, removes a file
// outside the root, or "." empties the root itself.
func TestRemoveRefusesANameNotCleanAndLocal(t *testing.T) {
	t.Parallel()
	root, dir := removeFolder(t)
	sep := string(filepath.Separator)
	for _, name := range []string{"", ".", "..", ".." + sep + "x", "tree" + sep + ".." + sep + "f.txt", "tree" + sep + "." + sep + "a.txt",
		"tree" + sep + sep + "a.txt", "tree" + sep, filepath.Join(dir, "f.txt")} {
		if err := Remove(root, name); !errors.Is(err, ErrOutside) {
			t.Errorf("Remove(%q): %v, and want ErrOutside", name, err)
		}
		if err := RemoveAll(root, name); !errors.Is(err, ErrOutside) {
			t.Errorf("RemoveAll(%q): %v, and want ErrOutside", name, err)
		}
	}
	holds(t, dir, "f.txt", "f")
	holds(t, dir, filepath.Join("tree", "a.txt"), "a")
}

// A link before the last component is refused by both with ErrOutside, though it leads inside the
// root, and the file it leads to stays.
//
// What it costs when this fails: a link planted in a folder a program clears leads the removal to
// another folder, outside the root where it leads there.
func TestRemoveRefusesALinkBeforeTheLastComponent(t *testing.T) {
	t.Parallel()
	root, dir := removeFolder(t)
	for _, name := range []string{filepath.Join("link", "HEAD"), filepath.Join("tree", "out", "HEAD")} {
		if err := Remove(root, name); !errors.Is(err, ErrOutside) {
			t.Errorf("Remove(%s): %v, and want ErrOutside", name, err)
		}
		if err := RemoveAll(root, name); !errors.Is(err, ErrOutside) {
			t.Errorf("RemoveAll(%s): %v, and want ErrOutside", name, err)
		}
	}
	holds(t, dir, filepath.Join("real", "HEAD"), "head")
}

// A folder swapped between RemoveAll's look at it and its open, to be emptied, is refused with
// ErrOutside, and the folder swapped in keeps what it holds.
//
// What it costs when this fails: a process that renames another folder in at the right moment has
// RemoveAll empty that folder, which it was never asked to touch.
func TestRemoveAllRefusesAFolderSwappedBeforeItIsEmptied(t *testing.T) {
	t.Parallel()
	for _, at := range []string{"tree", filepath.Join("tree", "sub"), filepath.Join("tree", "sub", "deeper")} {
		root, dir := removeFolder(t)
		err := removeAll(root, "tree", swapAt(t, dir, at), noOwner)
		if !errors.Is(err, ErrOutside) {
			t.Errorf("%s swapped before it was emptied: %v, and want ErrOutside", at, err)
		}
		holds(t, dir, filepath.Join(at, "keep.txt"), "keep")
	}
}

// swapAt swaps the folder at, below tree, for its namesake below beside when the walk has looked
// at it, before its open. The folder at is moved aside in its own folder, as a move to another
// would need its own write bit on Linux, and the walk has listed that folder already.
func swapAt(t *testing.T, dir, at string) func(step string) {
	t.Helper()
	looked := 0
	return func(step string) {
		if step != stepLooked {
			return
		}
		looked++
		if looked == 1+strings.Count(at, string(filepath.Separator)) {
			must(t, os.Rename(filepath.Join(dir, at), filepath.Join(dir, at+".aside")))
			must(t, os.Rename(filepath.Join(dir, "beside", relTo("tree", at)), filepath.Join(dir, at)))
		}
	}
}

// relTo is name's part below top, or "." for top itself.
func relTo(top, name string) string {
	rel, err := filepath.Rel(top, name)
	if err != nil {
		return name
	}
	return rel
}

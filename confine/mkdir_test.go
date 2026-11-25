package confine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// rootAt opens dir as a root, closed when the test ends.
func rootAt(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root
}

// Mkdir makes each missing folder of a name and answers the last as a root, in which a file made
// lands at the name; a name whose folders are all there makes and flushes nothing; and "." is the
// root again.
//
// What it costs when this fails: a program writes a file into a folder other than the one it
// named, or a power cut loses a folder Mkdir said it made.
func TestMkdirMakesEachMissingFolderAndAnswersTheLast(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := rootAt(t, dir)
	flushes := 0
	count := func(step string) {
		if step == stepFlushed {
			flushes++
		}
	}
	r, err := mkdir(root, filepath.Join("a", "b", "c"), 0o700, count, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.WriteFile("f", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.Close()
	if _, err := os.Stat(filepath.Join(dir, "a", "b", "c", "f")); err != nil {
		t.Errorf("the file made in the answered root is not at a/b/c/f: %v", err)
	}
	if flushes != 3 {
		t.Errorf("making three folders flushed %d folders above them, and want 3", flushes)
	}
	for _, c := range []struct {
		name string
		want int
	}{{filepath.Join("a", "b", "c"), 0}, {filepath.Join("a", "x", "y"), 2}} {
		flushes = 0
		r, err := mkdir(root, c.name, 0o700, count, false)
		if err != nil {
			t.Fatal(err)
		}
		r.Close()
		if flushes != c.want {
			t.Errorf("Mkdir %q flushed %d folders, and want %d", c.name, flushes, c.want)
		}
	}
	r, err = Mkdir(root, ".", 0o700)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.Stat(filepath.Join("a", "b", "c", "f")); err != nil {
		t.Errorf(`Mkdir "." is not the root: %v`, err)
	}
}

// A file, a link to a folder inside the root, or a dangling link, at any component, is refused
// with ErrOutside naming that component, and nothing is made where a link leads; so are a name
// that is not clean and local, and a mode beyond the nine permission bits.
//
// What it costs when this fails: a link a second account planted, state -> .git, has a folder made
// where it leads, and a later write lands there.
func TestMkdirRefusesALinkOrAFileAtAnyComponent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, d := range []string{"in", filepath.Join("a", "b")} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	plantLink(t, "in", filepath.Join(dir, "link"))
	plantLink(t, filepath.Join("..", "in"), filepath.Join(dir, "a", "link"))
	plantLink(t, "gone", filepath.Join(dir, "dangling"))
	root := rootAt(t, dir)
	for name, at := range map[string]string{
		filepath.Join("file", "x"):      "file",
		filepath.Join("link", "x"):      "link",
		"link":                          "link",
		filepath.Join("a", "link", "x"): "a/link",
		filepath.Join("dangling", "x"):  "dangling",
	} {
		r, err := Mkdir(root, name, 0o700)
		pe, ok := errors.AsType[*fs.PathError](err)
		if !errors.Is(err, ErrOutside) || !ok || pe.Path != at {
			t.Errorf("Mkdir %q: %v, and want ErrOutside naming %q", name, err, at)
		}
		if err == nil {
			r.Close()
		}
	}
	sep := string(filepath.Separator)
	if names, _ := os.ReadDir(filepath.Join(dir, "in")); len(names) != 0 {
		t.Errorf("a folder was made through a link: in holds %v", names)
	}
	if _, err := os.Lstat(filepath.Join(dir, "gone")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a folder was made where the dangling link leads: %v", err)
	}
	for _, name := range []string{".." + sep + "x", "a" + sep + ".." + sep + "b", "a" + sep + sep + "b", ""} {
		if _, err := Mkdir(root, name, 0o700); !errors.Is(err, ErrOutside) {
			t.Errorf("Mkdir %q: %v, and want ErrOutside", name, err)
		}
	}
	if _, err := Mkdir(root, "sticky", 0o700|fs.ModeSticky); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("Mkdir with a sticky bit: %v, and want fs.ErrInvalid", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "sticky")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a refused mode made the folder: %v", err)
	}
	for _, perm := range []fs.FileMode{0o300, 0o600, 0o077} {
		name := filepath.Join("blind", "x")
		if _, err := Mkdir(root, name, perm); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Mkdir %q with %v, which does not give this account read and search: %v, and want fs.ErrInvalid", name, perm, err)
		}
		if _, err := os.Lstat(filepath.Join(dir, "blind")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Mkdir with %v, refused, made a folder: %v", perm, err)
			if err := os.Chmod(filepath.Join(dir, "blind"), 0o700); err == nil {
				os.RemoveAll(filepath.Join(dir, "blind"))
			}
		}
	}
}

// A folder Mkdir has just made, swapped by another account that may write the folder above it for
// a link to a private file, a link to a private folder, or a private folder renamed from beside
// it, before its mode is set, is refused with ErrOutside: the target's mode stays, nothing is made
// in it, and the folder renamed in holds only its own file. The control, no swap, makes the
// folders.
//
// What it costs when this fails: a run's own secret is made readable by everyone, or a folder is
// made where the second account chose.
func TestMkdirRefusesAFolderSwappedBeforeItsModeIsSet(t *testing.T) {
	t.Parallel()
	for what, tc := range map[string]struct {
		target, name string
		rename       bool
	}{
		"a private file":                  {".env", filepath.Join("shared", "new", "f"), false},
		"a private folder":                {"private", filepath.Join("shared", "new", "deeper", "f"), false},
		"a private folder renamed beside": {filepath.Join("shared", "secret"), filepath.Join("shared", "new", "f"), true},
	} {
		for _, swap := range []bool{true, false} {
			dir := t.TempDir()
			for _, d := range []string{"private", filepath.Join("shared", "secret")} {
				if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, d, "x"), []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("secret"), 0o600); err != nil {
				t.Fatal(err)
			}
			made := filepath.Join(dir, "shared", "new")
			target := filepath.Join(dir, tc.target)
			mode := modeAt(t, target)
			if tc.rename {
				target = made
			}
			root := rootAt(t, dir)
			done := false
			r, err := mkdir(root, tc.name, 0o755, func(step string) {
				if step != stepMadeFolder || done || !swap {
					return
				}
				done = true
				if err := os.Remove(made); err != nil {
					t.Fatal(err)
				}
				if tc.rename {
					if err := os.Rename(filepath.Join(dir, tc.target), made); err != nil {
						t.Fatal(err)
					}
					return
				}
				plantLink(t, filepath.Join("..", tc.target), made)
			}, false)
			if err == nil {
				r.Close()
			}
			if !swap {
				if err != nil {
					t.Errorf("the control, Mkdir %q: %v, and want it made", tc.name, err)
				}
				continue
			}
			if !done {
				t.Fatalf("Mkdir %q never reached the step after a folder is made", tc.name)
			}
			if !errors.Is(err, ErrOutside) {
				t.Errorf("%s swapped in: %v, and want ErrOutside", what, err)
			}
			if got := modeAt(t, target); got != mode {
				t.Errorf("%s swapped in: its mode went from %v to %v", what, mode, got)
			}
			for _, d := range []string{filepath.Join(dir, "private"), filepath.Join(dir, tc.target), made} {
				if names := namesIn(d); names != nil && !slices.Equal(names, []string{"x"}) {
					t.Errorf("%s swapped in: %s holds %q, and want only its own x", what, d, names)
				}
			}
		}
	}
}

// modeAt is the mode of what is at path, not followed.
func modeAt(t *testing.T, path string) fs.FileMode {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode()
}

// namesIn is the names in the folder d, or nil when d is not a folder.
func namesIn(d string) []string {
	entries, err := os.ReadDir(d)
	if err != nil {
		return nil
	}
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

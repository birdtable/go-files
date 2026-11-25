package confine_test

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// files are what the folder holds beside its links: a folder standing for
// a repository's own, a file below another folder, and a file beside.
func files() map[string]string {
	return map[string]string{
		"real/HEAD":   "head\n",
		"real/config": "config\n",
		"deep/a.txt":  "a\n",
		"beside.txt":  "beside\n",
	}
}

// links are the links planted, each path to its target, relative to the
// link's own folder, all inside the root: a folder, a file, a target that
// is missing, and a link below a folder that is real.
func links() map[string]string {
	return map[string]string{
		"state":         "real",
		"history.jsonl": "real/config",
		"dangling":      "real/gone",
		"deep/via":      "../real",
	}
}

// plainFolder makes a folder holding files(), with no link, and opens it as a root.
func plainFolder(t *testing.T) (*os.Root, string) {
	t.Helper()
	dir := t.TempDir()
	for p, body := range files() {
		path := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root, dir
}

// folder makes a folder holding files() and links() and opens it as a root.
func folder(t *testing.T) (*os.Root, string) {
	t.Helper()
	root, dir := plainFolder(t)
	for p, to := range links() {
		confine.PlantLink(t, filepath.FromSlash(to), filepath.Join(dir, filepath.FromSlash(p)))
	}
	return root, dir
}

// unchanged fails the test unless every file of files() holds what it was
// made with, and the dangling link's missing target was not made.
func unchanged(t *testing.T, dir string) {
	t.Helper()
	n := 0
	for p, want := range files() {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil || string(got) != want {
			t.Errorf("%s holds %q, %v, want %q", p, got, err, want)
		}
		n++
	}
	if n == 0 {
		t.Fatal("no file was checked")
	}
	if _, err := os.Lstat(filepath.Join(dir, "real", "gone")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the dangling link's target was made: %v", err)
	}
}

// opens are the ways a file is opened: to read, to replace whole, to
// append, and to make.
func opens() map[string]int {
	return map[string]int{
		"read":    os.O_RDONLY,
		"replace": os.O_WRONLY | os.O_TRUNC,
		"append":  os.O_RDWR | os.O_APPEND | os.O_CREATE,
		"make":    os.O_WRONLY | os.O_CREATE | os.O_EXCL,
	}
}

// A link at any component of a path, the last included, is refused by
// NoLinks and by Open however the file is opened, naming the component
// that is a link and matching ErrOutside, and what the link leads to is
// left as it was.
//
// What it costs when this fails: a repository that commits state -> .git
// carries a write to state/HEAD into .git/HEAD, which os.Root alone allows.
func TestALinkAtAnyComponentIsRefused(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, path, link string }{
		{"a link as a middle component", "state/HEAD", "state"},
		{"a link as the last component", "history.jsonl", "history.jsonl"},
		{"a link to a target that is missing", "dangling", "dangling"},
		{"a link below a real folder", "deep/via/config", "deep/via"},
	}
	var ran atomic.Int32
	// The subtests run in parallel, so the count is read once they have all finished.
	t.Cleanup(func() {
		if ran.Load() == 0 {
			t.Error("no open was tried")
		}
	})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root, dir := folder(t)
			name := filepath.FromSlash(c.path)
			_, err := confine.NoLinks(root, name)
			refusedAt(t, "NoLinks("+c.path+")", err, c.link)
			tried := 0
			for how, flag := range opens() {
				f, err := confine.Open(root, name, flag, 0o644)
				if f != nil {
					_ = f.Close()
				}
				refusedAt(t, "Open("+c.path+") to "+how, err, c.link)
				ran.Add(1)
				tried++
			}
			if tried != 4 {
				t.Errorf("tried %d of the 4 ways to open %s", tried, c.path)
			}
			unchanged(t, dir)
		})
	}
}

// The control: the same files by their real paths, a file beside the
// links, and a new file below a folder that is real are read, replaced,
// appended to and made.
//
// What it costs when this fails: a refusal that is too wide stops a program from reading, replacing or
// making files that are its own, and the control that tells the two apart is gone.
func TestTheSameFilesByTheirRealPathsAreOpened(t *testing.T) {
	t.Parallel()
	root, dir := folder(t)
	write := func(path string, flag int, data string) {
		t.Helper()
		f, err := confine.Open(root, filepath.FromSlash(path), flag, 0o644)
		if err != nil {
			t.Errorf("Open(%s) = %v", path, err)
			return
		}
		defer f.Close()
		if _, err := io.WriteString(f, data); err != nil {
			t.Errorf("writing %s: %v", path, err)
		}
	}
	write("real/HEAD", os.O_WRONLY|os.O_TRUNC, "new\n")
	write("real/config", os.O_RDWR|os.O_APPEND|os.O_CREATE, "more\n")
	write("beside.txt", os.O_WRONLY|os.O_APPEND, "more\n")
	write("deep/b.txt", os.O_WRONLY|os.O_CREATE|os.O_EXCL, "b\n")
	write("fresh.txt", os.O_WRONLY|os.O_APPEND|os.O_CREATE, "fresh\n")
	checked := 0
	for p, want := range map[string]string{
		"real/HEAD":   "new\n",
		"real/config": "config\nmore\n",
		"beside.txt":  "beside\nmore\n",
		"deep/b.txt":  "b\n",
		"deep/a.txt":  "a\n",
		"fresh.txt":   "fresh\n",
	} {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil || string(got) != want {
			t.Errorf("%s holds %q, %v, want %q", p, got, err, want)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no file was checked")
	}
	f, err := confine.Open(root, filepath.FromSlash("real/HEAD"), os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Open(real/HEAD) to read = %v", err)
	}
	_ = f.Close()
	if info, err := confine.NoLinks(root, "."); err != nil || !info.IsDir() {
		t.Errorf("NoLinks(.) = %v, %v, want the root", info, err)
	}
}

// A component before the last that is not a folder is refused at that
// component, and it is not ErrOutside: nothing leads outside, the path is
// only wrong.
//
// What it costs when this fails: a wrong path is reported as an escape, or opens something, and the person
// is told the wrong thing about what went wrong.
func TestAComponentBeforeTheLastThatIsNotAFolderIsRefused(t *testing.T) {
	t.Parallel()
	root, _ := plainFolder(t)
	name := filepath.FromSlash("beside.txt/x")
	_, err := confine.NoLinks(root, name)
	pe, ok := errors.AsType[*fs.PathError](err)
	if !ok || pe.Path != "beside.txt" || errors.Is(err, confine.ErrOutside) {
		t.Errorf("NoLinks(beside.txt/x) = %v, want a refusal at beside.txt that is not ErrOutside", err)
	}
	if f, err := confine.Open(root, name, os.O_WRONLY|os.O_CREATE, 0o644); err == nil {
		_ = f.Close()
		t.Error("Open(beside.txt/x) opened something")
	}
}

// A name that is not clean and local is refused by NoLinks and by Open
// before anything is looked at, as ErrOutside.
//
// What it costs when this fails: a name that climbs out of the folder, or is absolute, reaches the walk, and
// a file outside the root is opened.
func TestANameThatIsNotCleanAndLocalIsRefused(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	bad := []string{"..", filepath.Join("..", "x"), "real" + string(filepath.Separator) + ".." + string(filepath.Separator) + "beside.txt",
		filepath.Join(dir, "beside.txt"), "", "real" + string(filepath.Separator)}
	ran := 0
	for _, name := range bad {
		ran++
		if _, err := confine.NoLinks(root, name); !errors.Is(err, confine.ErrOutside) {
			t.Errorf("NoLinks(%q) = %v, want ErrOutside", name, err)
		}
		if f, err := confine.Open(root, name, os.O_RDONLY, 0); !errors.Is(err, confine.ErrOutside) {
			if f != nil {
				_ = f.Close()
			}
			t.Errorf("Open(%q) = %v, want ErrOutside", name, err)
		}
	}
	if ran != len(bad) || ran == 0 {
		t.Fatalf("%d of %d names were tried", ran, len(bad))
	}
	unchanged(t, dir)
}

// A missing file is made only when the open asks to make it; without
// os.O_CREATE it is fs.ErrNotExist and nothing is made, and an exclusive
// create of a file that is there is fs.ErrExist.
//
// What it costs when this fails: a read or a write that did not ask to make a file makes one, or an
// exclusive create overwrites a file that is there.
func TestAMissingFileIsMadeOnlyWhenAskedFor(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	tried := 0
	for how, flag := range map[string]int{"read": os.O_RDONLY, "write": os.O_WRONLY, "replace": os.O_WRONLY | os.O_TRUNC} {
		tried++
		f, err := confine.Open(root, "missing", flag, 0o644)
		if f != nil {
			_ = f.Close()
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Open(missing) to %s = %v, want fs.ErrNotExist", how, err)
		}
	}
	if tried == 0 {
		t.Fatal("no open was tried")
	}
	if _, err := os.Lstat(filepath.Join(dir, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("an open that did not ask to make a file made one: %v", err)
	}
	if f, err := confine.Open(root, "beside.txt", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644); !errors.Is(err, fs.ErrExist) {
		if f != nil {
			_ = f.Close()
		}
		t.Errorf("an exclusive create of beside.txt = %v, want fs.ErrExist", err)
	}
	unchanged(t, dir)
}

// A name with a component ..namedfork, in any case, is refused by NoLinks, by Open however the file
// is opened, and by both renames, matching ErrOutside, on every system: macOS reads
// beside.txt/..namedfork/rsrc as beside.txt's resource fork, which its kernel open serves with no
// link on the way. beside.txt keeps its bytes, and no fork is written to it.
//
// What it costs when this fails: a caller that checks a name with NoLinks, or by listing the
// folder, and then calls Open, writes bytes on macOS that no listing of the root shows.
func TestANamedForkIsRefusedOnEverySystem(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	tried := 0
	for _, p := range []string{"beside.txt/..namedfork/rsrc", "beside.txt/..NAMEDFORK/rsrc", "beside.txt/..namedfork/data", "..namedfork"} {
		name := filepath.FromSlash(p)
		if _, err := confine.NoLinks(root, name); !errors.Is(err, confine.ErrOutside) {
			t.Errorf("NoLinks(%s) = %v, want ErrOutside", p, err)
		}
		for how, flag := range opens() {
			f, err := confine.Open(root, name, flag|os.O_CREATE, 0o644)
			if f != nil {
				_, _ = f.WriteString("fork")
				_ = f.Close()
			}
			if !errors.Is(err, confine.ErrOutside) {
				t.Errorf("Open(%s) to %s = %v, want ErrOutside", p, how, err)
			}
			tried++
		}
		for what, rename := range map[string]func(*os.Root, string, string) error{
			"RenameNoReplace": confine.RenameNoReplace, "RenameExchange": confine.RenameExchange,
		} {
			if err := rename(root, name, "moved.txt"); !errors.Is(err, confine.ErrOutside) {
				t.Errorf("%s(%s, moved.txt) = %v, want ErrOutside", what, p, err)
			}
		}
	}
	if tried == 0 {
		t.Fatal("tried no open")
	}
	if got, err := os.ReadFile(filepath.Join(dir, "beside.txt")); err != nil || string(got) != "beside\n" {
		t.Errorf("beside.txt holds %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "beside.txt", "..namedfork", "rsrc")); err == nil && len(got) != 0 {
		t.Errorf("beside.txt's fork holds %q", got)
	}
}

//go:build linux || darwin || windows

package confine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// renames are the calls that act on a name, each run on from and to: RenameExchange only where the
// system swaps two names in one step, swaps.
func renames() map[string]func(root *os.Root, from, to string) error {
	calls := map[string]func(root *os.Root, from, to string) error{"RenameNoReplace": confine.RenameNoReplace}
	if swaps {
		calls["RenameExchange"] = confine.RenameExchange
	}
	return calls
}

// twins puts twin.txt and hard, two names of one file, in dir.
func twins(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "twin.txt"), []byte("twin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(dir, "twin.txt"), filepath.Join(dir, "hard")); err != nil {
		t.Fatal(err)
	}
}

// RenameNoReplace moves a file, and a folder, to a missing name, and refuses a name that is there,
// a file or a folder, with fs.ErrExist, leaving both as they were.
//
// What it costs when this fails: a rename that was to keep what is there replaces it, as a plain
// rename does, and a file another process made a moment before is lost.
func TestRenameNoReplaceMovesToAMissingNameAndRefusesATakenOne(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	if err := confine.RenameNoReplace(root, filepath.Join("deep", "a.txt"), filepath.Join("real", "a.txt")); err != nil {
		t.Fatalf("RenameNoReplace of a file to a missing name: %v", err)
	}
	if err := confine.RenameNoReplace(root, "deep", "deeper"); err != nil {
		t.Fatalf("RenameNoReplace of a folder to a missing name: %v", err)
	}
	cases := []struct{ what, from, to string }{
		{"a file onto a file", filepath.Join("real", "a.txt"), "beside.txt"},
		{"a file onto a folder", filepath.Join("real", "a.txt"), "deeper"},
		{"a folder onto a file", "deeper", "beside.txt"},
		{"a folder onto a folder", "deeper", "real"},
	}
	for _, c := range cases {
		if err := confine.RenameNoReplace(root, c.from, c.to); !errors.Is(err, fs.ErrExist) || errors.Is(err, confine.ErrOutside) {
			t.Errorf("RenameNoReplace of %s: %v, want fs.ErrExist", c.what, err)
		}
	}
	if len(cases) != 4 {
		t.Fatalf("held %d cases", len(cases))
	}
	want := map[string]string{"real/a.txt": "a\n", "beside.txt": "beside\n", "real/HEAD": "head\n", "real/config": "config\n"}
	for p, body := range want {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil || string(got) != body {
			t.Errorf("%s holds %q, %v, want %q", p, got, err, body)
		}
	}
	if info, err := os.Lstat(filepath.Join(dir, "deeper")); err != nil || !info.IsDir() {
		t.Errorf("deeper is %v, %v, want the moved folder", info, err)
	}
	for _, gone := range []string{"deep", filepath.Join("real", "deeper")} {
		if _, err := os.Lstat(filepath.Join(dir, gone)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s is there: %v", gone, err)
		}
	}
}

// Both calls refuse, with an error matching ErrOutside, a file with a second name, a hard link, at
// either name, a name that climbs or is not clean, and the root itself, and move nothing. This runs
// on every system, with no link planted.
//
// What it costs when this fails: a hard link to a file the program guards is moved over, or a name
// from a file moves something outside the folder.
func TestARenameRefusesAHardLinkAndANameThatIsNotClean(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	twins(t, dir)
	cases := []struct{ what, from, to string }{
		{"a hard link moved", "hard", "moved"},
		{"onto a hard link", "beside.txt", "twin.txt"},
		{"a name that climbs", filepath.Join("..", "x"), "moved"},
		{"onto a name that climbs", "beside.txt", filepath.Join("..", "x")},
		{"a name that is not clean", "real" + string(filepath.Separator) + ".." + string(filepath.Separator) + "beside.txt", "moved"},
		{"the root", ".", "moved"},
		{"onto the root", "beside.txt", "."},
	}
	tried := 0
	for call, run := range renames() {
		for _, c := range cases {
			if err := run(root, c.from, c.to); !errors.Is(err, confine.ErrOutside) {
				t.Errorf("%s of %s: %v, want ErrOutside", call, c.what, err)
			}
			tried++
		}
	}
	if tried != len(renames())*len(cases) {
		t.Fatalf("tried %d of %d", tried, len(renames())*len(cases))
	}
	unchanged(t, dir)
	for _, name := range []string{"twin.txt", "hard"} {
		if got, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(got) != "twin\n" {
			t.Errorf("%s holds %q, %v", name, got, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "moved")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("moved is there: %v", err)
	}
}

// Both calls refuse, with an error matching ErrOutside, a link at a middle component of either
// name and a link at the last, and move nothing.
//
// What it costs when this fails: a repository that commits state -> .git has a rename of
// state/HEAD move .git/HEAD, or a rename onto a link replace the file it leads to.
func TestARenameRefusesALinkOnTheWay(t *testing.T) {
	t.Parallel()
	root, dir := folder(t)
	cases := []struct{ what, from, to string }{
		{"through a link in the middle", filepath.Join("state", "HEAD"), "moved"},
		{"onto a name through a link in the middle", "beside.txt", filepath.Join("state", "moved")},
		{"a link at the last component", "history.jsonl", "moved"},
		{"onto a link at the last component", "beside.txt", "dangling"},
		{"a link below a real folder", filepath.Join("deep", "via", "config"), "moved"},
	}
	tried := 0
	for call, run := range renames() {
		for _, c := range cases {
			if err := run(root, c.from, c.to); !errors.Is(err, confine.ErrOutside) {
				t.Errorf("%s %s: %v, want ErrOutside", call, c.what, err)
			}
			tried++
		}
	}
	if tried != len(renames())*len(cases) {
		t.Fatalf("tried %d of %d", tried, len(renames())*len(cases))
	}
	unchanged(t, dir)
	for _, gone := range []string{"moved", filepath.Join("real", "moved")} {
		if _, err := os.Lstat(filepath.Join(dir, gone)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s is there: %v", gone, err)
		}
	}
	if info, err := os.Lstat(filepath.Join(dir, "history.jsonl")); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("history.jsonl is %v, %v, want the link as it was", info, err)
	}
}

// RenameNoReplace refuses a component before the last of either name that is not a folder, and
// the refusal is not ErrOutside: nothing leads outside, the path is only wrong. Nothing moves.
//
// What it costs when this fails: a wrong path is reported as an escape, or a file is moved into,
// or out of, something that is not a folder.
func TestARenameRefusesAComponentBeforeTheLastThatIsNotAFolder(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	cases := []struct{ what, from, to string }{
		{"from a file's child", filepath.Join("beside.txt", "x"), "moved"},
		{"to a file's child", filepath.Join("real", "HEAD"), filepath.Join("beside.txt", "x")},
	}
	for _, c := range cases {
		if err := confine.RenameNoReplace(root, c.from, c.to); err == nil || errors.Is(err, confine.ErrOutside) {
			t.Errorf("RenameNoReplace %s: %v, want a refusal that is not ErrOutside", c.what, err)
		}
	}
	unchanged(t, dir)
	if _, err := os.Lstat(filepath.Join(dir, "moved")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("moved is there: %v", err)
	}
}

// A folder moved into itself, or swapped with its own child or parent, is the caller's own error.
// On Linux and macOS it is EINVAL in an *os.LinkError, as os.Rename gives, and never
// errors.ErrUnsupported or ErrNoExchange: Linux's renameat2 answers EINVAL both for this and for a
// file system that does not take the flag. On Windows the move is the system's own refusal, not
// errors.ErrUnsupported, and the swap is ErrNoExchange, as every swap there is. Nothing moves.
//
// What it costs when this fails: a caller told that the file system cannot do the move takes
// another path, such as a lock and a plain rename, for a call that is refused for its own names.
func TestAFolderRenamedIntoItselfIsTheCallersErrorNotUnsupported(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	if err := os.Mkdir(filepath.Join(dir, "real", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join("real", "sub")
	calls := []struct {
		what     string
		exchange bool
		err      error
	}{
		{"RenameNoReplace(real, real/moved)", false, confine.RenameNoReplace(root, "real", filepath.Join("real", "moved"))},
		{"RenameExchange(real, real/sub)", true, confine.RenameExchange(root, "real", sub)},
		{"RenameExchange(real/sub, real)", true, confine.RenameExchange(root, sub, "real")},
	}
	for _, c := range calls {
		var link *os.LinkError
		switch {
		case c.err == nil:
			t.Errorf("%s moved, want a refusal", c.what)
		case swaps: // Linux and macOS
			if !errors.Is(c.err, syscall.EINVAL) || !errors.As(c.err, &link) || errors.Is(c.err, errors.ErrUnsupported) {
				t.Errorf("%s = %v, want EINVAL in an *os.LinkError, neither errors.ErrUnsupported nor ErrNoExchange", c.what, c.err)
			}
		case c.exchange: // Windows, which swaps nothing
			if !errors.Is(c.err, confine.ErrNoExchange) {
				t.Errorf("%s = %v, want ErrNoExchange", c.what, c.err)
			}
		case errors.Is(c.err, errors.ErrUnsupported): // Windows
			t.Errorf("%s = %v, want the system's own refusal, not errors.ErrUnsupported", c.what, c.err)
		}
	}
	if info, err := os.Lstat(filepath.Join(dir, sub)); err != nil || !info.IsDir() {
		t.Errorf("real/sub is %v, %v, want the folder where it was", info, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "real", "moved")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("real/moved is there: %v", err)
	}
}

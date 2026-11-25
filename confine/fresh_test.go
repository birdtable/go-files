package confine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// inTurn answers each of seq in turn, then the last again, and counts the calls.
func inTurn(seq ...string) (random func() string, calls *int) {
	n := 0
	return func() string {
		at := min(n, len(seq)-1)
		n++
		return seq[at]
	}, &n
}

// noStep is a reached that does nothing.
func noStep(string) {}

// MkdirFresh makes one empty folder in the root, named by the prefix and 16 lower-case hex digits,
// a name Portable passes, and answers it opened as a root, in which a file made lands in that very
// folder; two calls never share a name.
//
// What it costs when this fails: a caller's scratch folder is named so a folder that folds case or
// a Windows volume reads it as another, is not where the answered root makes files, or two jobs
// share one.
func TestMkdirFreshMakesOneEmptyFolderUnderThePrefix(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"", "build-", "Ünï-", strings.Repeat("a", maxName-2*freshBytes)} {
		dir := t.TempDir()
		root := rootAt(t, dir)
		got, name, err := MkdirFresh(root, prefix)
		if err != nil {
			t.Fatalf("MkdirFresh %q: %v", prefix, err)
		}
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok || len(rest) != 2*freshBytes || strings.Trim(rest, "0123456789abcdef") != "" {
			t.Errorf("MkdirFresh %q named it %q, and want the prefix and 16 lower-case hex digits", prefix, name)
		}
		if _, err := Portable(name); err != nil {
			t.Errorf("MkdirFresh %q named it %q: %v", prefix, name, err)
		}
		if names := namesIn(dir); !slices.Equal(names, []string{name}) {
			t.Errorf("MkdirFresh %q: the root holds %q, and want only %q", prefix, names, name)
		}
		if names := namesIn(filepath.Join(dir, name)); len(names) != 0 {
			t.Errorf("MkdirFresh %q: the folder holds %q, and want it empty", prefix, names)
		}
		if err := got.WriteFile("in", []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		got.Close()
		if names := namesIn(filepath.Join(dir, name)); !slices.Equal(names, []string{"in"}) {
			t.Errorf("MkdirFresh %q: a file made in the answered root left %s holding %q", prefix, name, names)
		}
		again, other, err := MkdirFresh(root, prefix)
		if err != nil {
			t.Fatal(err)
		}
		again.Close()
		if other == name {
			t.Errorf("MkdirFresh %q twice answered %q both times", prefix, name)
		}
	}
}

// A taken name, whether a folder, a file or a link, is tried again under another, and what is there
// is not opened, followed, changed or answered.
//
// What it costs when this fails: another account that plants a folder at the name a caller will
// pick, or a link to its own, is handed the caller's scratch files.
func TestMkdirFreshTriesATakenNameAgainAndLeavesItAsItWas(t *testing.T) {
	t.Parallel()
	const taken, free = "1111111111111111", "2222222222222222"
	for _, what := range []string{"folder", "file", "link"} {
		dir := t.TempDir()
		elsewhere := t.TempDir()
		at := filepath.Join(dir, "job-"+taken)
		switch what {
		case "folder":
			if err := os.Mkdir(at, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(at, "kept"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		case "file":
			if err := os.WriteFile(at, []byte("kept"), 0o600); err != nil {
				t.Fatal(err)
			}
		case "link":
			plantLink(t, elsewhere, at)
		}
		before, err := os.Lstat(at)
		if err != nil {
			t.Fatal(err)
		}
		random, calls := inTurn(taken, free)
		got, name, err := mkdirFresh(rootAt(t, dir), "job-", random, noStep, false)
		if err != nil {
			t.Fatalf("a %s at the first name: %v", what, err)
		}
		got.Close()
		if name != "job-"+free || *calls != 2 {
			t.Errorf("a %s at the first name: answered %q after %d names, and want job-%s after 2", what, name, *calls, free)
		}
		after, err := os.Lstat(at)
		if err != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() || !after.ModTime().Equal(before.ModTime()) {
			t.Errorf("the %s at the first name changed: %v, %v, and want it as it was", what, after, err)
		}
		switch what {
		case "folder":
			if names := namesIn(at); !slices.Equal(names, []string{"kept"}) {
				t.Errorf("the folder planted holds %q, and want only kept", names)
			}
		case "file":
			if body, err := os.ReadFile(at); err != nil || string(body) != "kept" {
				t.Errorf("the file planted holds %q (%v), and want kept", body, err)
			}
		case "link":
			if names := namesIn(elsewhere); len(names) != 0 {
				t.Errorf("the link's target holds %q, and want it empty", names)
			}
		}
	}
}

// Every name taken, MkdirFresh is refused with fs.ErrExist after its tries, and nothing is made.
//
// What it costs when this fails: a caller that cannot have a fresh folder is handed one that was
// there, or the call never ends.
func TestMkdirFreshRefusesWhenEveryNameIsTaken(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "job-1111111111111111"), 0o700); err != nil {
		t.Fatal(err)
	}
	random, calls := inTurn("1111111111111111")
	got, name, err := mkdirFresh(rootAt(t, dir), "job-", random, noStep, false)
	if err == nil {
		got.Close()
	}
	if !errors.Is(err, fs.ErrExist) || name != "" || *calls != freshTries {
		t.Errorf("every name taken: %q, %v after %d names, and want fs.ErrExist after %d", name, err, *calls, freshTries)
	}
	if names := namesIn(dir); !slices.Equal(names, []string{"job-1111111111111111"}) {
		t.Errorf("every name taken: the root holds %q, and want only the folder planted", names)
	}
}

// A prefix with a separator, a character Windows reserves, a format character, or .git in any case,
// or one that makes the name longer than 255 bytes, is refused with fs.ErrInvalid, and nothing is
// made.
//
// What it costs when this fails: a caller's prefix leads into another folder, names a folder git
// takes for a repository's, or names one a Windows or macOS volume reads as another.
func TestMkdirFreshRefusesABadPrefixAndMakesNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := rootAt(t, dir)
	for _, prefix := range []string{"a/b", `a\b`, "a:b", "a*b", "a\x00b", "a" + string(rune(0x200b)) + "b", ".git", ".GIT", strings.Repeat("a", maxName-2*freshBytes+1), "\xff"} {
		got, _, err := MkdirFresh(root, prefix)
		if err == nil {
			got.Close()
		}
		if !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("MkdirFresh %q: %v, and want fs.ErrInvalid", prefix, err)
		}
	}
	if names := namesIn(dir); len(names) != 0 {
		t.Errorf("bad prefixes: the root holds %q, and want it empty", names)
	}
}

// A folder holding a file renamed in at the name, or a link put there, between MkdirFresh's make and
// its open, is refused with ErrOutside, and nothing is done to it, as with Mkdir.
//
// What it costs when this fails: an account that may write the root swaps its own folder in for
// the fresh one, and the caller's files land where it reads them.
func TestMkdirFreshRefusesAFolderSwappedInForTheOneMade(t *testing.T) {
	t.Parallel()
	for what, link := range map[string]bool{"a folder holding a file renamed in": false, "a link": true} {
		dir := t.TempDir()
		for _, held := range []string{"beside", "private"} {
			if err := os.Mkdir(filepath.Join(dir, held), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, held, "x"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		made := filepath.Join(dir, "job-1111111111111111")
		done := false
		random, _ := inTurn("1111111111111111")
		got, _, err := mkdirFresh(rootAt(t, dir), "job-", random, func(step string) {
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
			got.Close()
		}
		if !done || !errors.Is(err, ErrOutside) {
			t.Errorf("%s swapped in for the folder MkdirFresh made (reached %v): %v, and want ErrOutside", what, done, err)
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

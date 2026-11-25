package confine

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A missing name another process makes while Resolve walks the path, and a link swapped then, are
// refused with ErrChanged, which matches ErrOutside; a refusal that is not a change, as a path
// that is not absolute, does not match ErrChanged.
//
// What it costs when this fails: a caller cannot tell a second run making its state folder from a
// path that leads outside, and refuses the harmless race as hostile, or takes a hostile refusal
// as the race.
func TestAPathThatChangedWhileItWasWalkedIsErrChanged(t *testing.T) {
	t.Parallel()
	base, err := Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(base, "state")
	_, _, err = resolveChecked(state, func() {
		if err := os.Mkdir(state, 0o700); err != nil {
			t.Error(err)
		}
	})
	if !errors.Is(err, ErrChanged) || !errors.Is(err, ErrOutside) {
		t.Errorf("Resolve with its missing tail made after the walk = %v, want ErrChanged and ErrOutside", err)
	}
	if err := os.Mkdir(filepath.Join(base, "in"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "l")
	plantLink(t, "in", link)
	_, _, err = resolveChecked(link, func() {
		if err := os.Remove(link); err != nil {
			t.Error(err)
		}
		plantLink(t, "state", link)
	})
	if !errors.Is(err, ErrChanged) || !errors.Is(err, ErrOutside) {
		t.Errorf("Resolve with its link swapped after the walk = %v, want ErrChanged and ErrOutside", err)
	}
	if _, err := Resolve("relative"); !errors.Is(err, ErrOutside) || errors.Is(err, ErrChanged) {
		t.Errorf("Resolve of a relative path = %v, want ErrOutside and not ErrChanged", err)
	}
}

// A folder Mkdir made that another process puts a name in before it is checked is refused with
// ErrNotEmpty, which matches ErrOutside, and nothing in it is touched; a link swapped in for the
// folder made is ErrOutside and not ErrNotEmpty, and on Unix so is a folder another account's.
//
// What it costs when this fails: a caller that takes "made, and filled by a second run" as the
// folder that was there takes a link or another account's folder too, or cannot tell the race at
// all and stays refused.
func TestAFolderMadeAndFilledBeforeItsCheckIsErrNotEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := rootAt(t, dir)
	made := filepath.Join(dir, "state")
	done := false
	r, err := mkdir(root, "state", 0o700, func(step string) {
		if step != stepMadeFolder || done {
			return
		}
		done = true
		if err := os.WriteFile(filepath.Join(made, "x"), []byte("x"), 0o600); err != nil {
			t.Error(err)
		}
	}, false)
	if err == nil {
		r.Close()
	}
	if !done {
		t.Fatal("Mkdir never reached the step after a folder is made")
	}
	if !errors.Is(err, ErrNotEmpty) || !errors.Is(err, ErrOutside) {
		t.Errorf("Mkdir with its folder filled after the make = %v, want ErrNotEmpty and ErrOutside", err)
	}
	if names := namesIn(made); len(names) != 1 || names[0] != "x" {
		t.Errorf("the filled folder holds %q, want only its x", names)
	}

	if err := os.Mkdir(filepath.Join(dir, "elsewhere"), 0o700); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(dir, "linked")
	done = false
	r, err = mkdir(root, "linked", 0o700, func(step string) {
		if step != stepMadeFolder || done {
			return
		}
		done = true
		if err := os.Remove(linked); err != nil {
			t.Error(err)
		}
		plantLink(t, "elsewhere", linked)
	}, false)
	if err == nil {
		r.Close()
	}
	if !done {
		t.Fatal("Mkdir never reached the step after a folder is made")
	}
	if !errors.Is(err, ErrOutside) || errors.Is(err, ErrNotEmpty) {
		t.Errorf("Mkdir with a link swapped in for its folder = %v, want ErrOutside and not ErrNotEmpty", err)
	}

	f, err := os.Open(filepath.Join(dir, "elsewhere"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	held, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := uidOf(held); !ok {
		return // Windows: a folder's owner is a SID, held by listedAs, which ErrNotEmpty is never
	}
	if err := fresh(f, held, "elsewhere", os.Geteuid()+1); !errors.Is(err, ErrOutside) || errors.Is(err, ErrNotEmpty) {
		t.Errorf("the fresh check of a folder another account's = %v, want ErrOutside and not ErrNotEmpty", err)
	}
}

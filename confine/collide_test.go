package confine_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// openRoot opens dir as a root, closed when the test ends.
func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

// folds is what the system's own look says of a and b in a fresh folder: a made, then looked
// for as b.
func folds(t *testing.T, a, b string) bool {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, a), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := os.Lstat(filepath.Join(dir, b))
	return err == nil
}

// leftEmpty holds dir to have no entry.
func leftEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Errorf("the folder holds %v, %v, want nothing", entries, err)
	}
}

// A name that is not one name in the folder is refused before anything is looked at.
//
// What it costs when this fails: Collide(root, "x", "../x") makes and removes a probe outside
// the folder it was asked about, and answers for another folder.
func TestCollideRefusesANameThatIsNotOneName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := openRoot(t, dir)
	for _, name := range []string{"", ".", "..", "a/b", "../a", "/a"} {
		for _, pair := range [][2]string{{name, "x"}, {"x", name}} {
			if got, err := confine.Collide(root, pair[0], pair[1]); !errors.Is(err, confine.ErrOutside) {
				t.Errorf("Collide(%q, %q) = %v, %v, want a refusal matching ErrOutside", pair[0], pair[1], got, err)
			}
		}
	}
	leftEmpty(t, dir)
}

// With neither name there, Collide answers as the system's own look does, for case and for é
// composed and decomposed, and leaves the folder as it found it.
//
// What it costs when this fails: a product that asks whether two names in a folder are one is
// answered by the system's name, not by its volume, or finds the probe left behind.
func TestCollideAnswersAsTheFolderReadsAndLeavesNothing(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]string{{"Fold", "fold"}, {"\u00e9", "e\u0301"}, {"a", "b"}} {
		dir := t.TempDir()
		got, err := confine.Collide(openRoot(t, dir), pair[0], pair[1])
		if want := folds(t, pair[0], pair[1]); err != nil || got != want {
			t.Errorf("Collide(%+q, %+q) = %v, %v, want %v, as the system's own look", pair[0], pair[1], got, err, want)
		}
		t.Logf("this volume reads %+q and %+q as one: %v", pair[0], pair[1], got)
		leftEmpty(t, dir)
	}
	if got, err := confine.Collide(openRoot(t, t.TempDir()), "x", "x"); err != nil || !got {
		t.Errorf("Collide(x, x) = %v, %v, want true", got, err)
	}
}

// What is there is looked at with no link followed, and left there: a link to a is not a, a is A
// only where the volume folds case, and a file at a is not removed.
//
// What it costs when this fails: a link b to a reads as a second name for a, and the folder is
// judged to fold; or a caller's own file at a is removed as a probe.
func TestCollideLooksAtWhatIsThereWithNoLinkFollowed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := openRoot(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	plantLink(t, "a", filepath.Join(dir, "l"))
	for _, pair := range [][2]string{{"a", "l"}, {"l", "a"}, {"a", "b"}, {"b", "a"}} {
		if got, err := confine.Collide(root, pair[0], pair[1]); err != nil || got {
			t.Errorf("Collide(%s, %s) = %v, %v, want false", pair[0], pair[1], got, err)
		}
	}
	if got, err := confine.Collide(root, "a", "A"); err != nil || got != folds(t, "a", "A") {
		t.Errorf("Collide(a, A) with a there = %v, %v, want %v, as the system's own look", got, err, !got)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "a")); err != nil || string(b) != "a" {
		t.Errorf("a holds %q, %v, want it left as it was", b, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "b")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("b is %v, want it never made", err)
	}
}

// A file put at a in place of the probe, between its make and its removal, is left where it is.
//
// What it costs when this fails: the probe's removal takes another's file made at the name in
// that time.
func TestCollideLeavesAFilePutInPlaceOfItsProbe(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	at := filepath.Join(dir, "probe")
	if _, err := confine.CollideMade(openRoot(t, dir), "probe", "other", func() {
		if err := os.Remove(at); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(at, []byte("theirs"), 0o600); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(at); err != nil || string(b) != "theirs" {
		t.Errorf("probe holds %q, %v, want the file put in its place left there", b, err)
	}
}

// A file put at b by another while the probe is made is not the probe, and the answer is false.
//
// What it costs when this fails: any file found at b is taken for the probe, and a folder that
// folds nothing is judged to fold.
func TestCollideTakesOnlyItsProbeForB(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got, err := confine.CollideMade(openRoot(t, dir), "probe", "other", func() {
		if err := os.WriteFile(filepath.Join(dir, "other"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if err != nil || got {
		t.Errorf("Collide(probe, other) with other made meanwhile = %v, %v, want false", got, err)
	}
}

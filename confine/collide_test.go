package confine_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
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

// A file made or removed between Collide's looks, as another call's probe is, changes what it sees
// at one name and not at the other. Collide looks at both again before it answers apart, so the
// answer is the folder's own: true where the volume folds Fold and fold, false where it does not.
// Each race is set by the looked hook, after the look it names, on the first try alone:
//   - Fold there, and removed once a is looked at;
//   - nothing there, and fold made once a is looked at and removed once b is, as a probe of
//     another call's comes and goes;
//   - nothing there, and fold made or removed after every look at a, so the folder changes under
//     each try: where the volume folds, the third answers true and a is looked at three times.
//
// What it costs when this fails: on a volume that folds them, Collide answers that Fold and fold
// are apart because another call's probe came and went between its looks, and a caller that keeps
// two files apart by those names has one overwrite the other.
func TestCollideAnswersAsTheFolderReadsWhatChangesBetweenItsLooks(t *testing.T) {
	t.Parallel()
	want := folds(t, "Fold", "fold")
	for _, c := range []struct {
		name   string
		before []string
		race   func(t *testing.T, dir string, step string, try int)
	}{
		{"a removed after its look", []string{"Fold"}, func(t *testing.T, dir, step string, try int) {
			t.Helper()
			if step == "a" && try == 1 {
				remove(t, filepath.Join(dir, "Fold"))
			}
		}},
		{"a probe at fold made after a's look and removed after b's", nil, func(t *testing.T, dir, step string, try int) {
			t.Helper()
			switch {
			case step == "a" && try == 1:
				make0(t, filepath.Join(dir, "fold"))
			case step == "b" && try == 1:
				remove(t, filepath.Join(dir, "fold"))
			}
		}},
		{"fold made or removed after every look at a", nil, func(t *testing.T, dir, step string, _ int) {
			t.Helper()
			if step != "a" {
				return
			}
			at := filepath.Join(dir, "fold")
			if _, err := os.Lstat(at); err == nil {
				remove(t, at)
			} else {
				make0(t, at)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for _, name := range c.before {
				make0(t, filepath.Join(dir, name))
			}
			tries := 0
			got, err := confine.CollideLooked(openRoot(t, dir), "Fold", "fold", func(step string) {
				if step == "a" {
					tries++
				}
				c.race(t, dir, step, tries)
			})
			if err != nil || got != want {
				t.Errorf("Collide(Fold, fold), %s = %v, %v, want %v (the folder folds them: %v)", c.name, got, err, want, want)
			}
			if c.before == nil && want && c.name == "fold made or removed after every look at a" && tries != 3 {
				t.Errorf("a looked at %d times, want 3, the third try's true", tries)
			}
		})
	}
}

// Collide raced by itself: calls on one folder at once, each one's probe coming and going between
// the others' looks. Where the volume folds Fold and fold, no call answers apart. Where it does
// not, a call whose three tries each saw the folder change answers one name, the fail-closed
// answer, and that is allowed; but some call answers apart, as the folder reads them.
//
// What it costs when this fails: two goroutines asking Collide of one folder at once, as a tool
// checking names in parallel does, are told now and then that names the volume takes for one are
// two; or, where the volume folds nothing, every raced call says one name, and Collide tells the
// folder nothing.
func TestCollideRacedByItselfAnswersAsTheFolderReads(t *testing.T) {
	t.Parallel()
	want := folds(t, "Fold", "fold")
	dir := t.TempDir()
	root := openRoot(t, dir)
	const callers, calls = 8, 200
	var mu sync.Mutex
	answered, closed, wrong := 0, 0, 0
	var refused []error
	var wg sync.WaitGroup
	for i := range callers {
		a, b := "Fold", "fold"
		if i%2 == 1 {
			a, b = b, a
		}
		wg.Go(func() {
			for range calls {
				got, err := confine.Collide(root, a, b)
				mu.Lock()
				switch {
				case err != nil:
					refused = append(refused, err)
				case want && !got:
					wrong++
				case !want && got:
					closed++
				default:
					answered++
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if wrong != 0 {
		t.Errorf("%d of %d calls answered apart, where the folder folds Fold and fold", wrong, callers*calls)
	}
	if answered == 0 {
		t.Errorf("no call of %d answered %v, as the folder reads Fold and fold; %d answered one name, and the rest gave an error: %v",
			callers*calls, want, closed, errors.Join(refused...))
	}
	if closed > 0 {
		t.Logf("%d of %d calls answered one name, where the folder does not fold Fold and fold: each saw it change under three tries", closed, callers*calls)
	}
	if len(refused) > 0 {
		t.Logf("%d of %d calls gave an error, the first: %v", len(refused), callers*calls, refused[0])
	}
}

// make0 makes an empty file at path.
func make0(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Error(err)
	}
}

// remove removes the file at path.
func remove(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Error(err)
	}
}

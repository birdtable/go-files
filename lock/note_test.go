package lock

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The note is written, read, removed and named at the caller's path, which is not beside the lock
// file and not the lock's name with a suffix, and nothing is written at <name>.holder.
//
// What it costs when this fails: the library names a file of its own in the lock's folder again,
// where the program never chose one and its readers do not look.
func TestTheNoteIsAtTheCallersPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.lock")
	if err := os.Mkdir(filepath.Join(dir, "notes"), 0o700); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(dir, "notes", "who")
	l, err := Take(path, note)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Release() })
	if err := l.Note("mine"); err != nil {
		t.Fatal(err)
	}
	if h, raw, err := Holder(note); err != nil || h.By != os.Getpid() || string(raw) != `"mine"` {
		t.Fatalf("Holder at the caller's path: %+v, %s, %v", h, raw, err)
	}
	var h Held
	if _, err := Take(path, note); !errors.As(err, &h) || h.By != os.Getpid() {
		t.Fatalf("a second Take with the caller's note: %v, and want Held by %d", err, os.Getpid())
	}
	for _, name := range []string{path + ".holder", path + ".1", filepath.Join(dir, "app.holder")} {
		if _, err := os.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: %v, and want no file the library named", name, err)
		}
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(note); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the note after Release: %v, and want it removed", err)
	}
}

// A lock taken with note "" writes, reads and removes no note: Note is refused with ErrNoNote and
// writes nothing, a second Take is declined naming no holder, and a note an earlier holder left
// at <name>.holder is neither read nor removed, by Take, Wait or Release.
//
// What it costs when this fails: a program that named no note has the library write, read or
// remove one at a name it never gave, which the default of no note rules out.
func TestALockWithNoNoteWritesReadsAndRemovesNone(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.lock")
	left, err := json.Marshal(note{PID: 1, Since: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	left = append(left, '\n')
	if err := os.WriteFile(path+".holder", left, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, take := range map[string]func() (*Lock, error){
		"Take": func() (*Lock, error) { return Take(path, "") },
		"Wait": func() (*Lock, error) { return Wait(t.Context(), path, "") },
	} {
		l, err := take()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := l.Note("mine"); !errors.Is(err, ErrNoNote) {
			t.Errorf("%s, then Note: %v, and want ErrNoNote", name, err)
		}
		var h Held
		if _, err := Take(path, ""); !errors.As(err, &h) || h != (Held{}) {
			t.Errorf("%s, then a second Take: %v, %+v, and want Held{}", name, err, h)
		}
		if err := l.Release(); err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(path + ".holder"); err != nil || string(got) != string(left) {
			t.Fatalf("%s and Release with no note left <name>.holder %q, %v, and want it untouched", name, got, err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 {
			t.Fatalf("%s with no note left %d names in the folder, and want the lock and the old note", name, len(entries))
		}
	}
}

// A note path that is the lock file: one that cleans to the lock's path is refused by Take and
// Wait before anything is opened. A second name of the lock file, there before Take or made once
// the lock is held, is left alone by Take and Wait, which win the lock, and refused by Note and
// Release, which leave the lock file and that name as they are.
//
// What it costs when this fails: Note's rename or Release's removal takes the lock file's own
// name, and the next Take makes a second lock file, so two runs hold the lock at once; or
// whoever can make that name in the note's folder keeps every run from the lock.
func TestANoteThatIsTheLockFileIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.lock")
	for _, note := range []string{path, filepath.Join(dir, ".", "app.lock"), filepath.Join(dir, "x", "..", "app.lock")} {
		if l, err := Take(path, note); l != nil || !errors.Is(err, errNoteIsLock) {
			t.Errorf("Take with note %s: %v, %v, and want the refusal", note, l, err)
			_ = l.Release() // a lock that was not refused is let go, so the next row is not left waiting on it
		}
		if l, err := Wait(t.Context(), path, note); l != nil || !errors.Is(err, errNoteIsLock) {
			t.Errorf("Wait with note %s: %v, %v, and want the refusal", note, l, err)
			_ = l.Release()
		}
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused note made the lock file: %v", err)
	}

	if err := take(t, path).Release(); err != nil {
		t.Fatal(err)
	}
	was, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(dir, "second")
	for _, c := range []struct {
		name   string
		before bool // the second name is there before Take, not made once the lock is held
		take   func() (*Lock, error)
	}{
		{"Take", true, func() (*Lock, error) { return Take(path, second) }},
		{"Wait", true, func() (*Lock, error) { return Wait(t.Context(), path, second) }},
		{"Take, the name made once held", false, func() (*Lock, error) { return Take(path, second) }},
	} {
		if err := os.Remove(second); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if c.before {
			if err := os.Link(path, second); err != nil {
				t.Fatal(err)
			}
		}
		l, err := c.take()
		if err != nil {
			t.Fatalf("%s with a second name of the lock file for its note: %v, and want the lock", c.name, err)
		}
		if !c.before {
			if err := os.Link(path, second); err != nil {
				t.Fatal(err)
			}
		}
		if err := l.Note("mine"); !errors.Is(err, errNoteIsLock) {
			t.Errorf("%s, then Note at a second name of the lock file: %v, and want the refusal", c.name, err)
		}
		if err := l.Release(); !errors.Is(err, errNoteIsLock) {
			t.Errorf("%s, then Release with its note at a second name of the lock file: %v, and want the refusal", c.name, err)
		}
		if fi, err := os.Lstat(path); err != nil || !os.SameFile(fi, was) || fi.Size() != 0 {
			t.Fatalf("%s: the lock file after the refusals: %v, %v, and want it as it was", c.name, fi, err)
		}
		if fi, err := os.Lstat(second); err != nil || !os.SameFile(fi, was) {
			t.Fatalf("%s: the note's name after the refusals: %v, %v, and want it the lock file still", c.name, fi, err)
		}
	}
}

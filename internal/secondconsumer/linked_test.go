package secondconsumer_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/birdtable/go-files/confine"
	"github.com/birdtable/go-files/lock"
	"github.com/birdtable/go-files/write"
)

// namesIn fails t unless dir holds want and nothing else: no temp file left beside a write.
func namesIn(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(entries))
	for _, e := range entries {
		got = append(got, e.Name())
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("%s holds %v, and want %v alone", dir, got, want)
	}
}

// The watcher keeps its settings in a folder of their own and links them into place, and writes
// them through the link by the recipe write's README gives: confine.Resolve answers where the link
// leads, the target missing or there, and write.File replaces that file whole. The target holds
// the new settings, the link is still the link, and no temp file is left beside either. One link
// is to the settings' folder, a junction on Windows, and one to the file itself, on every system:
// a link to a folder on the way is followed by any write, so the link to the file is the one a
// write that skipped Resolve would replace.
//
// What it costs when this fails: a program that links its settings into place replaces the link
// with a plain file, so its next run reads settings no other place sees, or follows a link it was
// never meant to, where write follows none.
func TestTheWatchersSettingsLinkedIntoPlaceAreWrittenThroughByResolve(t *testing.T) {
	t.Parallel()
	for _, there := range []bool{true, false} {
		dir := t.TempDir()
		kept := filepath.Join(dir, "kept-settings")
		if err := os.Mkdir(kept, 0o700); err != nil {
			t.Fatal(err)
		}
		if there {
			if err := os.WriteFile(filepath.Join(kept, settingsName), []byte("cone 5\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		if err := confine.LinkFolder(root, kept, "settings"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(kept, settingsName), filepath.Join(dir, settingsName)); err != nil {
			t.Fatal(err) // on Windows a test account has developer mode on, and a refused link fails
		}
		for how, path := range map[string]string{
			"through a link to its folder": filepath.Join(dir, "settings", settingsName),
			"through a link to the file":   filepath.Join(dir, settingsName),
		} {
			body := []byte("cone 6, " + how + "\n")
			resolved, err := confine.Resolve(path)
			if err != nil {
				t.Errorf("confine.Resolve %s (there %v): %v", how, there, err)
				continue
			}
			if err := write.File(resolved, body, settingsOptions()...); err != nil {
				t.Errorf("write.File %s (there %v): %v", how, there, err)
				continue
			}
			if got, err := os.ReadFile(filepath.Join(kept, settingsName)); err != nil || string(got) != string(body) {
				t.Errorf("the kept settings, written %s (there %v): %q, %v, and want %q", how, there, got, err, body)
			}
		}
		if got, err := confine.Readlink(root, "settings"); err != nil || got != kept {
			t.Errorf("the folder link after the writes (there %v): %q, %v, and want it still the link to %q", there, got, err, kept)
		}
		if fi, err := os.Lstat(filepath.Join(dir, settingsName)); err != nil || fi.Mode()&fs.ModeSymlink == 0 {
			t.Errorf("the file link after the writes (there %v): %v, and want it still a link", there, err)
		}
		namesIn(t, dir, "kept-settings", "settings", settingsName)
		namesIn(t, kept, settingsName)
	}
}

// A lock file the watcher may not open to write, one made read-only, is refused by lock.Take with
// an error matching fs.ErrPermission, never as held, and the settings it writes twice under
// write.NoReplace are refused the second time with an error matching fs.ErrExist, the first left
// as written: the standard library's errors, with no error of go-files' own to match in their
// place.
//
// What it costs when this fails: a program leaving its own lock and write packages matches a
// named error go-files does not have, or is told another run holds a lock it may not write.
func TestTheWatchersLockItMayNotWriteAndSettingsWrittenTwiceMatchTheStandardErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, lockName)
	if err := os.WriteFile(path, nil, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { // the read-only attribute on Windows would refuse the folder's removal
		if err := os.Chmod(path, 0o600); err != nil {
			t.Error(err)
		}
	})
	l, err := lock.Take(path, filepath.Join(dir, noteName))
	if l != nil {
		t.Error("lock.Take took a lock file this account may not write")
		if err := l.Release(); err != nil {
			t.Error(err)
		}
	}
	if !errors.Is(err, fs.ErrPermission) || lock.IsHeld(err) || !strings.Contains(err.Error(), path) {
		t.Errorf("lock.Take of a lock file this account may not write: %v, and want fs.ErrPermission naming it, not held", err)
	}

	settings := filepath.Join(dir, settingsName)
	if err := write.File(settings, []byte("cone 6\n"), write.NoReplace()); err != nil {
		t.Fatal(err)
	}
	if err := write.File(settings, []byte("cone 10\n"), write.NoReplace()); !errors.Is(err, fs.ErrExist) {
		t.Errorf("write.File under NoReplace over the settings: %v, and want fs.ErrExist", err)
	}
	if got, err := os.ReadFile(settings); err != nil || string(got) != "cone 6\n" {
		t.Errorf("the settings after the refused write: %q, %v, and want the first", got, err)
	}
	namesIn(t, dir, lockName, settingsName)
}

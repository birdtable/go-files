package secondconsumer_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/birdtable/go-files/appdirs"
	"github.com/birdtable/go-files/confine"
	"github.com/birdtable/go-files/filestest"
	"github.com/birdtable/go-files/lock"
	"github.com/birdtable/go-files/treesum"
	"github.com/birdtable/go-files/trust"
	"github.com/birdtable/go-files/write"
)

// The kiln watcher's own values, each unlike what birdtable's programs hand go-files.
const (
	app          = "kiln-watch"
	settingsName = "firing.toml"
	settingsMode = 0o640 // its group reads the settings
	atticMode    = 0o750
	keptName     = "before-firing-0001.kiln"
	logName      = "firings.log"
	logAside     = "firings.prev.log"
	logMode      = 0o640
	logCap       = 96 // bytes: a few firings a log
	lockName     = "east-kiln.lock"
	lockMode     = 0o660                 // the kiln group may take it
	noteName     = "who-fires-east.json" // where the watcher notes who holds the kiln, in a folder of its own
	recipes      = "glazes"
	waitFor      = 30 * time.Millisecond // the watcher stands down fast while another holds the kiln
	appendFor    = 250 * time.Millisecond
	readCap      = 200 // bytes of settings the watcher reads
)

// settingsOptions is how the watcher writes its settings: group-readable where the system has
// modes, and with no mode on Windows, where the file has the owner-only access list whatever
// mode is asked.
func settingsOptions() []write.Option {
	if !modes {
		return nil
	}
	return []write.Option{write.Mode(settingsMode)}
}

// logOptions is how the watcher keeps its log: moved aside to a name of its own, and
// group-readable where the system has modes, at first and after each move.
func logOptions() []write.LogOption {
	opts := []write.LogOption{write.Aside(logAside)}
	if modes {
		opts = append(opts, write.LogMode(logMode))
	}
	return opts
}

// theAccountsAlone fails t unless trust passes path as kind, which it refuses where another account
// may read or write it, and path is owned by the account that runs the watcher. The watcher reads
// a Windows list no closer than trust does; internal/winapi's tests read each list back.
func theAccountsAlone(t *testing.T, path string, kind trust.Kind) {
	t.Helper()
	if _, err := trust.Path(path, kind, trust.Who{}); err != nil {
		t.Errorf("trust judges %s: %v, and want it this account's alone", path, err)
	}
	me, err := trust.Account()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := trust.OwnerOf(f)
	if cerr := f.Close(); cerr != nil {
		t.Fatal(cerr)
	}
	if err != nil || owner != me {
		t.Errorf("%s is owned by %s (%v), and want this account, %s", path, owner, err, me)
	}
}

// The watcher chooses its macOS layout, as appdirs takes none for it, and keeps no socket, so it
// goes on past ErrRun where its session has no XDG_RUNTIME_DIR, with Run "".
//
// What it costs when this fails: go-files names, places or reads a program's folders by a value
// of its own, not the one the program hands it.
func TestTheWatchersFoldersAreNamedFromWhatItHandsAppdirs(t *testing.T) {
	t.Parallel()
	env := map[string]string{"XDG_STATE_HOME": "/srv/kiln/state", "LOCALAPPDATA": `D:\Kilns`}
	get := func(k string) string { return env[k] }
	for _, c := range []struct {
		goos, home string
		layout     appdirs.Layout
		state, run string
	}{
		{"linux", "/home/potter", appdirs.XDG, "/srv/kiln/state/" + app, ""},
		{"freebsd", "/usr/home/potter", appdirs.XDG, "/srv/kiln/state/" + app, ""},
		{"darwin", "/Users/potter", appdirs.XDG, "/srv/kiln/state/" + app, ""},
		{"darwin", "/Users/potter", appdirs.Native, "/Users/potter/Library/Application Support/" + app, "/Users/potter/Library/Application Support/" + app},
		{"windows", `C:\Users\potter`, appdirs.XDG, `D:\Kilns\` + app + `\State`, `D:\Kilns\` + app + `\Run`},
	} {
		f, err := appdirs.For(c.goos, app, c.home, get, c.layout)
		if err != nil && !errors.Is(err, appdirs.ErrRun) || errors.Is(err, appdirs.ErrRun) != (c.run == "") || f.State != c.state || f.Run != c.run {
			t.Errorf("%s, layout %d: State %q, Run %q, %v, want %q and %q", c.goos, c.layout, f.State, f.Run, err, c.state, c.run)
		}
	}
	withRun := func(k string) string {
		if k == "XDG_RUNTIME_DIR" {
			return "/run/user/1000"
		}
		return get(k)
	}
	if f, err := appdirs.For("linux", app, "/home/potter", withRun, appdirs.XDG); err != nil || f.Run != "/run/user/1000/"+app {
		t.Errorf("linux with XDG_RUNTIME_DIR: Run %q, %v", f.Run, err)
	}
	if _, err := appdirs.For("darwin", app, "/Users/potter", get, 0); !errors.Is(err, appdirs.ErrLayout) {
		t.Errorf("no layout chosen: %v, want ErrLayout", err)
	}
	if _, err := appdirs.For("linux", "kiln/watch", "/home/potter", get, appdirs.XDG); !errors.Is(err, appdirs.ErrApp) {
		t.Errorf("a name with a separator: %v, want ErrApp", err)
	}
	dir := t.TempDir()
	if free, err := appdirs.Free(dir); err != nil || free == 0 {
		t.Errorf("room in the watcher's folder: %d, %v", free, err)
	}
}

// What it costs when this fails: go-files writes, keeps or restores a program's settings with a
// mode, a folder or a check of its own, not the program's.
func TestTheWatchersSettingsAreWrittenKeptAndReadByItsOwnValues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	settings := filepath.Join(dir, settingsName)
	first := []byte("cone = 6\n")
	if err := write.File(settings, first, settingsOptions()...); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(settings); err != nil {
		t.Fatal(err)
	} else if modes && fi.Mode().Perm() != settingsMode {
		t.Fatalf("the settings the watcher wrote: mode %o, want %o", fi.Mode().Perm(), settingsMode)
	}

	attic := filepath.Join(dir, "attic")
	if err := os.Mkdir(attic, atticMode); err != nil {
		t.Fatal(err)
	}
	kept, err := write.Keep(settings, attic, keptName) // the watcher names its copies by firing, not by time
	if err != nil || kept != filepath.Join(attic, keptName) {
		t.Fatalf("the copy: %q, %v, want %s in %s", kept, err, keptName, attic)
	}
	if _, err := write.Keep(settings, attic, keptName); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("a second copy at the watcher's taken name %s: %v, want fs.ErrExist", keptName, err)
	}
	second := []byte("cone = 10\n")
	check := write.Check(func(f *os.File) error {
		var b bytes.Buffer
		if _, err := b.ReadFrom(f); err != nil {
			return err
		}
		if !strings.HasPrefix(b.String(), "cone = ") {
			return errors.New("not a firing")
		}
		return nil
	})
	if err := write.File(settings, second, append(settingsOptions(), write.IfHolds(sha256.Sum256(first)), check)...); err != nil {
		t.Fatal(err)
	}
	if err := write.File(settings, []byte("glaze\n"), append(settingsOptions(), check)...); err == nil {
		t.Error("the watcher's own check let a file that is not a firing in")
	}
	if err := write.PutBack(kept, settings, sha256.Sum256(first)); !errors.Is(err, write.ErrChanged) {
		t.Errorf("a restore over a change: %v, want ErrChanged", err)
	}
	if err := write.PutBack(kept, settings, sha256.Sum256(second)); err != nil {
		t.Fatal(err)
	}

	owner, err := trust.Account()
	if err != nil {
		t.Fatal(err)
	}
	if b, err := trust.ReadOwned(settings, owner, false, readCap); err != nil || !bytes.Equal(b, first) {
		t.Errorf("the restored settings read back: %q, %v, want %q", b, err, first)
	}
	if _, err := trust.ReadOwned(settings, owner, false, len(first)-1); !errors.Is(err, trust.ErrTooLong) {
		t.Errorf("settings over the watcher's cap: %v, want ErrTooLong", err)
	}
}

// What it costs when this fails: go-files tells a program it kept a file's mode or owner where it
// kept neither, or that it flushed a folder it did not.
func TestTheWatchersSettingsKeepTheirModeAndOwnerAndItsAtticIsFlushed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	settings := filepath.Join(dir, settingsName)
	if err := write.File(settings, []byte("cone = 6\n"), settingsOptions()...); err != nil {
		t.Fatal(err)
	}
	err := write.File(settings, []byte("cone = 8\n"), write.KeepMode())
	if fi, serr := os.Stat(settings); modes && (err != nil || serr != nil || fi.Mode().Perm() != settingsMode) {
		t.Errorf("the watcher's settings rewritten keeping their mode: %v, %v, and want %o", err, serr, settingsMode)
	} else if !modes {
		if err != nil {
			t.Errorf("KeepMode over the watcher's owner-only settings on Windows: %v", err)
		}
		theAccountsAlone(t, settings, trust.Key)
	}
	if err := write.File(settings, []byte("cone = 9\n"), write.KeepOwner()); modes && err != nil {
		t.Errorf("the watcher's settings rewritten keeping their owner: %v", err)
	} else if !modes {
		if err != nil {
			t.Errorf("KeepOwner over the watcher's own settings on Windows: %v", err)
		}
		theAccountsAlone(t, settings, trust.Key)
	}

	attic := filepath.Join(dir, "attic")
	if err := os.Mkdir(attic, atticMode); err != nil {
		t.Fatal(err)
	}
	if _, err := write.Keep(settings, attic, keptName); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(attic)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	if err := root.Remove(keptName); err != nil {
		t.Fatal(err)
	}
	if err := write.SyncFolder(root, "."); err != nil { // the watcher's copy is gone for good
		t.Errorf("the watcher's attic flushed after it removed %s: %v", keptName, err)
	}
}

// The watcher's old log goes to the name it hands Aside, and a line logged with no Aside is refused.
//
// What it costs when this fails: go-files caps a program's log, waits on it, or names where its
// old log goes, by a value of its own, not the program's.
func TestTheWatchersLogIsCappedAndWaitedOnByItsOwnValues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(t.Context(), appendFor)
	defer cancel()
	line := []byte("firing 0001: cone 6, 1222 C, 9 h 40 min")
	for range 3 {
		if err := write.Append(ctx, root, logName, line, logCap, logOptions()...); err != nil {
			t.Fatal(err)
		}
	}
	moved, err := root.Stat(logAside)
	if err != nil {
		t.Fatalf("the log moved aside to %s at the watcher's cap of %d: %v", logAside, logCap, err)
	}
	if moved.Size() != 2*int64(len(line)+1) {
		t.Fatalf("the log moved aside holds %d bytes, want two lines' %d", moved.Size(), 2*(len(line)+1))
	}
	if modes && moved.Mode().Perm() != logMode {
		t.Errorf("the log the watcher made: mode %o, want %o", moved.Mode().Perm(), logMode)
	}
	fi, err := root.Stat(logName)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != int64(len(line)+1) {
		t.Errorf("the new log holds %d bytes, want one line's %d", fi.Size(), len(line)+1)
	}
	if modes && fi.Mode().Perm() != logMode {
		t.Errorf("the log the watcher made after the move: mode %o, want %o", fi.Mode().Perm(), logMode)
	}
	if _, err := root.Lstat(logName + ".1"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a log was put at %s.1, not at the watcher's %s: %v", logName, logAside, err)
	}
	if err := write.Append(ctx, root, logName, line, logCap); !errors.Is(err, write.ErrNoAside) {
		t.Errorf("a line logged with no Aside: %v, want ErrNoAside, as Append names no old log of its own", err)
	}
	if again, err := root.Stat(logName); err != nil || again.Size() != fi.Size() {
		t.Errorf("the log after a line with no Aside: %v, %v, want it as it was", again, err)
	}
}

// The watcher takes its lock with no note, and then writes one of its own, at the path it names,
// in a folder of its own.
//
// What it costs when this fails: go-files locks a program's kiln, notes who holds it, or waits for
// it, by a value of its own, not the program's, writes a note the program did not ask for, or
// names the note's file itself.
func TestTheWatchersLockIsTakenNotedAndWaitedOnByItsOwnValues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, lockName)
	if err := os.Mkdir(filepath.Join(dir, "firing"), atticMode); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(dir, "firing", noteName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, lockMode)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(f.Chmod(lockMode), f.Close()); err != nil {
		t.Fatal(err)
	}
	l, err := lock.Take(path, note)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	if _, err := os.Lstat(note); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the watcher took its lock with no note and found one at its note's name: %v", err)
	}
	if _, err := lock.Take(path, note); !lock.IsHeld(err) || err.Error() != (lock.Held{}).Error() {
		t.Errorf("a second watcher beside a lock with no note: %v, want Held{}, naming nobody", err)
	}
	type firing struct {
		Kiln string `json:"kiln"`
		Cone int    `json:"cone"`
	}
	if err := l.Note(firing{Kiln: "east", Cone: 6}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path + ".holder", filepath.Join(dir, "firing", lockName+".holder")} {
		if _, err := os.Lstat(name); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("the watcher's note was written at %s, a name it never gave: %v", name, err)
		}
	}
	held, raw, err := lock.Holder(note)
	var got firing
	if err != nil || held.By != os.Getpid() || json.Unmarshal(raw, &got) != nil || got.Kiln != "east" {
		t.Errorf("the holder: %+v, %s, %v", held, raw, err)
	}
	began := time.Now() // before the deadline is set, so no gap between the two shortens the wait measured
	ctx, cancel := context.WithTimeout(t.Context(), waitFor)
	defer cancel()
	_, err = lock.Wait(ctx, path, note)
	if !lock.IsHeld(err) || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a second watcher waiting %v: %v, want Held and the deadline", waitFor, err)
	}
	if waited := time.Since(began); waited < waitFor || waited > time.Second {
		t.Errorf("a second watcher waited %v, not about its own %v", waited, waitFor)
	}
	if fi, err := os.Stat(path); err != nil {
		t.Error(err)
	} else if modes && fi.Mode().Perm() != lockMode {
		t.Errorf("the lock file the watcher made: mode %o, want %o", fi.Mode().Perm(), lockMode)
	}
	if lf, err := l.File(); lf != nil || !errors.Is(err, lock.ErrNoInherit) {
		t.Errorf("the lock file to hand a child, which no child would hold the lock by: %v, %v, want ErrNoInherit", lf, err)
	}
}

// What it costs when this fails: go-files confines a program's names, or pins its tree, by a
// value of its own, not the program's.
func TestTheWatchersRecipesAreConfinedAndPinnedByItsOwnCaps(t *testing.T) {
	t.Parallel()
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.Mkdir(recipes, atticMode); err != nil {
		t.Fatal(err)
	}
	put := func(slash, body string, flag int) {
		t.Helper()
		name, err := confine.Local(slash)
		if err != nil {
			t.Fatal(err)
		}
		f, err := confine.Open(root, name, os.O_WRONLY|flag, settingsMode)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(body); err != nil {
			t.Fatal(errors.Join(err, f.Close()))
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	put("glazes/celadon.txt", "iron 1%\n", os.O_CREATE|os.O_EXCL)
	put("glazes/tenmoku.txt", "iron 10%\n", os.O_CREATE|os.O_EXCL)
	if _, err := confine.Local("../kilns/west"); !errors.Is(err, confine.ErrOutside) {
		t.Errorf("a name out of the recipes: %v, want ErrOutside", err)
	}
	if fi, err := confine.NoLinks(root, recipes); err != nil || fi == nil || !fi.IsDir() {
		t.Errorf("the recipes' folder, walked with no link: %v", err)
	}
	from, to := filepath.Join(recipes, "celadon.txt"), filepath.Join(recipes, "tenmoku.txt")
	if err := confine.RenameNoReplace(root, from, to); !errors.Is(err, fs.ErrExist) {
		t.Errorf("a recipe moved over another: %v, want ErrExist", err)
	}

	caps := treesum.Caps{Entries: 8, Bytes: 1 << 11} // far below what birdtable's programs allow
	digest, listing, err := treesum.Sum(root, recipes, caps)
	if err != nil {
		t.Fatal(err)
	}
	if err := treesum.Check(root, recipes, caps, digest, listing); err != nil {
		t.Errorf("the recipes as pinned: %v", err)
	}
	if _, _, err := treesum.Sum(root, recipes, treesum.Caps{Entries: 1, Bytes: caps.Bytes}); !errors.Is(err, treesum.ErrCap) {
		t.Errorf("the recipes past a cap of one entry: %v, want ErrCap", err)
	}
	put("glazes/celadon.txt", "iron 2%\n", os.O_TRUNC)
	if err := treesum.Check(root, recipes, caps, digest, listing); !errors.Is(err, treesum.ErrDiffers) {
		t.Errorf("a recipe changed since its pin: %v, want ErrDiffers", err)
	}
}

// What it costs when this fails: go-files finds a program's folder by a marker of its own only
// where the marker is one of ours, or walks past the ceiling the program names.
func TestTheWatcherFindsItsKilnByItsOwnMarkerBelowItsOwnCeiling(t *testing.T) {
	t.Parallel()
	shed := t.TempDir()
	kiln := filepath.Join(shed, "east")
	shelf := filepath.Join(kiln, "shelf", "3")
	if err := os.MkdirAll(filepath.Join(kiln, ".kiln"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(shelf, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kiln, ".kiln", "east.toml"), nil, settingsMode); err != nil {
		t.Fatal(err)
	}
	if got, err := confine.Nearest(shelf, ".kiln/east.toml", shed); err != nil || got != kiln {
		t.Errorf("the kiln above shelf 3: %q, %v, want %q", got, err, kiln)
	}
	if _, err := confine.Nearest(shelf, ".kiln/east.toml", filepath.Join(kiln, "shelf")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the kiln above the watcher's ceiling: %v, want fs.ErrNotExist", err)
	}
}

// What it costs when this fails: go-files reads the watcher's current-batch link only where the
// link is one of ours, or cannot tell its batch folder from a link.
func TestTheWatcherReadsItsCurrentBatchLinkByItsOwnNames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	batch := filepath.Join(dir, "batches", "2026-11-21")
	if err := os.MkdirAll(batch, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	if err := confine.LinkFolder(root, batch, "current"); err != nil {
		t.Fatal(err)
	}
	if got, err := confine.Readlink(root, "current"); err != nil || got != batch {
		t.Errorf("the current batch: %q, %v, want %q", got, err, batch)
	}
	if _, err := confine.Readlink(root, filepath.Join("batches", "2026-11-21")); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("the batch folder itself: %v, want fs.ErrInvalid", err)
	}
}

// What it costs when this fails: go-files refuses the watcher's own mode where the system has
// modes, or, on Windows, makes its attic with a list another account may read or write, where it
// holds the attic to its owner, narrower than the group read the watcher asks.
func TestTheWatchersAtticIsMadeByConfineWithItsOwnModeOrOwnerOnlyOnWindows(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	attic, err := confine.Mkdir(root, filepath.Join("attic", "2026"), atticMode)
	if !modes {
		if err != nil {
			t.Fatalf("the attic made %o on Windows: %v", atticMode, err)
		}
		if err := attic.Close(); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"attic", filepath.Join("attic", "2026")} {
			theAccountsAlone(t, filepath.Join(dir, name), trust.PrivateFolder)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := attic.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"attic", filepath.Join("attic", "2026")} {
		if fi, err := os.Stat(filepath.Join(dir, name)); err != nil || fi.Mode().Perm() != atticMode {
			t.Errorf("the attic folder %s the watcher made: %v, %v, want mode %o", name, fi, err, atticMode)
		}
	}
}

// atticRoot is a folder holding the watcher's attic of 2025, with one kept firing and a link to its
// glazes, which stay outside the attic, opened as a root closed when the test ends.
func atticRoot(t *testing.T) (*os.Root, string) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{filepath.Join("attic", "2025", keptName), filepath.Join(recipes, "celadon.toml")} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), atticMode); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("cone 10\n"), settingsMode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join("..", "..", recipes), filepath.Join(dir, "attic", "2025", recipes)); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root, dir
}

// The watcher drops one kept firing by confine.Remove, and the link to its glazes by its own name,
// leaving the glazes; a firing already gone is fs.ErrNotExist.
//
// What it costs when this fails: go-files removes a name only by the names birdtable's programs
// hand it, or follows the watcher's link into the glazes it keeps.
func TestTheWatchersKeptFiringIsRemovedByConfine(t *testing.T) {
	t.Parallel()
	root, dir := atticRoot(t)
	for _, name := range []string{filepath.Join("attic", "2025", keptName), filepath.Join("attic", "2025", recipes)} {
		if err := confine.Remove(root, name); err != nil {
			t.Errorf("the watcher's Remove(%s): %v", name, err)
		}
	}
	if err := confine.Remove(root, filepath.Join("attic", "2025", keptName)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the watcher's Remove of a firing already gone: %v, want fs.ErrNotExist", err)
	}
	if _, err := os.Stat(filepath.Join(dir, recipes, "celadon.toml")); err != nil {
		t.Errorf("the glazes the removed link led to: %v", err)
	}
}

// The watcher clears its attic of 2025 by confine.RemoveAll, and the link in it to its glazes is
// removed, not walked: the glazes stay. A year already cleared is nil.
//
// What it costs when this fails: clearing a year of the attic empties the glazes folder the
// watcher linked from it.
func TestTheWatchersAtticYearIsClearedByConfine(t *testing.T) {
	t.Parallel()
	root, dir := atticRoot(t)
	for range 2 {
		if err := confine.RemoveAll(root, filepath.Join("attic", "2025")); err != nil {
			t.Errorf("the watcher's RemoveAll(attic/2025): %v", err)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "attic", "2025")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the attic of 2025 after RemoveAll: %v, want it gone", err)
	}
	if _, err := os.Stat(filepath.Join(dir, recipes, "celadon.toml")); err != nil {
		t.Errorf("the glazes the attic linked to: %v", err)
	}
}

// What it costs when this fails: the watcher cannot publish the kiln's schedule to the studio's
// other accounts without a mode birdtable chose, or its own firing notes, made private by name, are
// reachable to them. Where the system has modes the schedule is 0644 in folders
// 0755, and the notes' folder 0700; on Windows the schedule and its folders hold what the folder
// above grants, and the notes' folder is the account's alone.
func TestTheWatchersScheduleIsSharedAndItsNotesPrivateByName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	board, err := confine.MkdirShared(root, filepath.Join("studio-board", "east"))
	if err != nil {
		t.Fatal(err)
	}
	if err := board.Close(); err != nil {
		t.Fatal(err)
	}
	schedule := filepath.Join(dir, "studio-board", "east", "cone-schedule.txt")
	if err := write.File(schedule, []byte("cone 6 at dusk\n"), write.Shared()); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(schedule); err != nil || string(b) != "cone 6 at dusk\n" {
		t.Errorf("the schedule holds %q (%v)", b, err)
	}
	notes, err := confine.MkdirPrivate(root, filepath.Join("firing-notes", "east"))
	if err != nil {
		t.Fatal(err)
	}
	if err := notes.Close(); err != nil {
		t.Fatal(err)
	}
	if !modes {
		for _, name := range []string{"firing-notes", filepath.Join("firing-notes", "east")} {
			theAccountsAlone(t, filepath.Join(dir, name), trust.PrivateFolder)
		}
		return
	}
	for name, want := range map[string]fs.FileMode{
		"studio-board":                                             0o755,
		filepath.Join("studio-board", "east"):                      0o755,
		filepath.Join("studio-board", "east", "cone-schedule.txt"): 0o644,
		"firing-notes":                                             0o700,
		filepath.Join("firing-notes", "east"):                      0o700,
	} {
		if fi, err := os.Stat(filepath.Join(dir, name)); err != nil || fi.Mode().Perm() != want {
			t.Errorf("%s the watcher made: %v, %v, want mode %o", name, fi, err, want)
		}
	}
}

// What it costs when this fails: the watcher cannot gather a firing's log in a scratch folder of
// its own, named so no other firing's job takes it, and move it into place whole, without a mode or
// a temp path birdtable chose, or the scratch folder is reachable to the studio's other accounts.
// Where the system has modes it is 0700; on Windows it is the account's alone.
func TestTheWatchersFiringLogIsGatheredInAFreshFolderAndMovedWhole(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	scratch, name, err := confine.MkdirFresh(root, "firing-0042.")
	if err != nil {
		t.Fatal(err)
	}
	if err := scratch.WriteFile("peaks.csv", []byte("cone,minute\n6,412\n"), 0o600); err != nil {
		t.Fatal(errors.Join(err, scratch.Close()))
	}
	if err := scratch.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "firing-0042.") {
		t.Errorf("the scratch folder is named %q, and want the firing's prefix", name)
	}
	if !modes {
		theAccountsAlone(t, filepath.Join(dir, name), trust.PrivateFolder)
	} else if fi, err := os.Stat(filepath.Join(dir, name)); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("the scratch folder the watcher made: %v, %v, want mode 700", fi, err)
	}
	if _, _, err := confine.MkdirFresh(root, "firing/0042."); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("a prefix with a separator: %v, want fs.ErrInvalid", err)
	}
	if err := confine.RenameNoReplace(root, name, "firing-0042"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "firing-0042", "peaks.csv")); err != nil || string(b) != "cone,minute\n6,412\n" {
		t.Errorf("the firing's log, moved whole, holds %q (%v)", b, err)
	}
}

// What it costs when this fails: the watcher's tests cannot write its settings and its kiln hook,
// and hand their folder to a trust walk, without naming a mode or a temp path, as a product's tests
// may not.
func TestTheWatchersTestsWriteItsHookInAPrivateFolderNamingNoMode(t *testing.T) {
	t.Parallel()
	dir := filestest.Private(t)
	filestest.WriteFile(t, filepath.Join(dir, settingsName), []byte("cone = 6\n"))
	filestest.WriteProgram(t, filepath.Join(dir, "on-peak"), []byte("#!/bin/sh\nexit 0\n"))
	_, err := trust.Path(dir, trust.PrivateFolder, trust.Who{Overflow: true})
	if errors.Is(err, trust.ErrOverflow) {
		_, err = trust.Path(dir, trust.PrivateFolder, trust.Who{})
	}
	if err != nil {
		t.Errorf("the watcher's test folder: %v, and want it judged this account's alone", err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, settingsName)); err != nil || string(b) != "cone = 6\n" {
		t.Errorf("the watcher's settings hold %q (%v)", b, err)
	}
}

package secondconsumer_test

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/appdirs"
	"github.com/birdtable/go-files/confine"
	"github.com/birdtable/go-files/lock"
	"github.com/birdtable/go-files/trust"
	"github.com/birdtable/go-files/write"
)

// On Windows each file and folder go-files makes for the watcher with no mode asked, its settings,
// the copy it keeps, its log, the lock it takes when none is there, its temp folder and a folder
// confine makes 0o700, is its account's, and trust, which refuses a name another account may read
// or write, passes each: a file as a key and a folder as a private folder. The watcher links
// nothing but go-files and the standard library, so it reads the access lists no closer than
// trust does; internal/winapi's tests read each list back and find SYSTEM and the owner alone,
// and each package's tests that its makes go through that one helper.
//
// What it costs when this fails: another account on the kiln's machine reads the watcher's
// settings and log, writes its lock, or plants files in its temp folder.
func TestTheWatchersPrivateFilesAreItsAccountsAloneOnWindows(t *testing.T) {
	t.Parallel()
	me, err := trust.Account()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	settings := filepath.Join(dir, settingsName)
	if err := write.File(settings, []byte("cone = 6\n")); err != nil {
		t.Fatal(err)
	}
	kept, err := write.Keep(settings, dir, keptName)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	if err := write.Append(t.Context(), root, logName, []byte("cone 6 reached"), logCap, logOptions()...); err != nil {
		t.Fatal(err)
	}
	l, err := lock.Take(filepath.Join(dir, lockName), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Release() }()
	firings, err := confine.Mkdir(root, filepath.Join("firings", "east"), 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if err := firings.Close(); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	temp, err := appdirs.Temp(app + "-" + hex.EncodeToString(b)) // a name of this run's alone, removed after
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(temp) })

	made := []struct {
		path string
		kind trust.Kind
	}{
		{settings, trust.Key},
		{kept, trust.Key},
		{filepath.Join(dir, logName), trust.Key},
		{filepath.Join(dir, lockName), trust.Key},
		{filepath.Join(dir, "firings"), trust.PrivateFolder},
		{filepath.Join(dir, "firings", "east"), trust.PrivateFolder},
		{temp, trust.PrivateFolder},
	}
	for _, m := range made {
		if _, err := trust.Path(m.path, m.kind, trust.Who{}); err != nil {
			t.Errorf("trust judges %s: %v, and want it this account's alone", m.path, err)
		}
		f, err := os.Open(m.path)
		if err != nil {
			t.Fatal(err)
		}
		owner, err := trust.OwnerOf(f)
		if cerr := f.Close(); cerr != nil {
			t.Fatal(cerr)
		}
		if err != nil || owner != me {
			t.Errorf("%s is owned by %s (%v), and want this account, %s", m.path, owner, err, me)
		}
	}
}

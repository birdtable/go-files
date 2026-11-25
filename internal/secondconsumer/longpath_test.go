package secondconsumer_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/birdtable/go-files/confine"
	"github.com/birdtable/go-files/lock"
	"github.com/birdtable/go-files/trust"
	"github.com/birdtable/go-files/write"
)

// longPast is the length a path passes here: the 260 of Windows' MAX_PATH, in bytes for one tree
// and in UTF-16 units for the other.
const longPast = 260

// The watcher keeps a recipe three folders deep, each name within Portable's 255, at a path of
// more than 260 bytes and at one of more than 260 UTF-16 units, and every call it takes there
// does it: confine.MkdirPrivate makes the folders, write.File and write.FileIn write a recipe,
// confine.Open reads it, confine.RenameNoReplace moves one, confine.Resolve and confine.Below
// answer it, lock.Take takes a lock there and trust.Path passes the recipe, and each is read
// back. On Windows the row logs LongPathsEnabled, read and never written, and runs at either
// value: it was proved with it set to 1, and a red where it is unset is a finding to report.
//
// What it costs when this fails: a deep testdata or state tree a product keeps works on one
// system and fails on Windows alone, past 260, where the product has no run of its own.
func TestTheWatchersDeepRecipesPastTwoHundredSixtyAreTakenByEveryCall(t *testing.T) {
	t.Parallel()
	if s := longPathsSetting(); s != "" {
		t.Log(s)
	}
	for _, deep := range []struct {
		top, part string
		units     bool
	}{
		{"bytes", strings.Repeat("a", 100), false},
		{"units", strings.Repeat("é", 90), true},
	} {
		if _, err := confine.Portable(deep.part); err != nil {
			t.Fatalf("%s: a name of the tree is not Portable's: %v", deep.top, err)
		}
		base := t.TempDir()
		root, err := os.OpenRoot(base)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		rel := filepath.Join(deep.top, deep.part, deep.part, deep.part)
		dir := filepath.Join(base, rel)
		one := filepath.Join(dir, "one.recipe")
		long := len(one)
		if deep.units {
			long = len(utf16.Encode([]rune(one)))
		}
		if long <= longPast {
			t.Fatalf("%s: the path is %d long, and the row holds one past %d", deep.top, long, longPast)
		}
		recipe := []byte("iron 10%\r\nash 30%\n\x00end")
		did := func(call string, err error) bool {
			t.Helper()
			if err != nil {
				t.Errorf("%s at %d %s: %v, and want it done", call, long, deep.top, err)
			}
			return err == nil
		}
		reads := func(call string, got []byte, err error) {
			t.Helper()
			if did(call+", read back", err) && !bytes.Equal(got, recipe) {
				t.Errorf("%s at %d %s reads back %q, and want %q", call, long, deep.top, got, recipe)
			}
		}

		made, err := confine.MkdirPrivate(root, rel)
		if !did("confine.MkdirPrivate", err) {
			continue
		}
		did("closing confine.MkdirPrivate's root", made.Close())

		if did("write.File", write.File(one, recipe)) {
			got, err := os.ReadFile(one)
			reads("write.File", got, err)
		}
		two := filepath.Join(rel, "two.recipe")
		if did("write.FileIn", write.FileIn(root, two, recipe)) {
			got, err := root.ReadFile(two)
			reads("write.FileIn", got, err)
		}
		if f, err := confine.Open(root, filepath.Join(rel, "one.recipe"), os.O_RDONLY, 0); did("confine.Open", err) {
			got, err := io.ReadAll(f)
			reads("confine.Open", got, err)
			did("closing confine.Open's file", f.Close())
		}
		three := filepath.Join(rel, "three.recipe")
		if did("confine.RenameNoReplace", confine.RenameNoReplace(root, two, three)) {
			got, err := root.ReadFile(three)
			reads("confine.RenameNoReplace", got, err)
		}

		top, err := confine.Resolve(base)
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(top, rel, "one.recipe")
		if got, err := confine.Resolve(one); did("confine.Resolve", err) && got != want {
			t.Errorf("confine.Resolve at %d %s answers %q, and want %q", long, deep.top, got, want)
		}
		if got, err := confine.Below(base, one); did("confine.Below", err) && got != filepath.Join(rel, "one.recipe") {
			t.Errorf("confine.Below at %d %s answers %q, and want %q", long, deep.top, got, filepath.Join(rel, "one.recipe"))
		}
		if l, err := lock.Take(filepath.Join(dir, lockName), filepath.Join(dir, noteName)); did("lock.Take", err) {
			did("lock.Release", l.Release())
		}
		if got, err := trust.Path(one, trust.File, trust.Who{}); did("trust.Path", err) && got != want {
			t.Errorf("trust.Path at %d %s answers %q, and want %q", long, deep.top, got, want)
		}
	}
}

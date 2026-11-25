package write

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// openRoot opens a root at dir, closed at the test's end.
func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

// apart fails the test unless neither of two folders is beneath the other, so neither root holds
// what the other does.
func apart(t *testing.T, a, b string) {
	t.Helper()
	for _, p := range [][2]string{{a, b}, {b, a}} {
		if rel, err := filepath.Rel(p[0], p[1]); err != nil || filepath.IsLocal(rel) || rel == "." {
			t.Fatalf("%s is beneath %s (%q, %v), and the row wants two folders apart", p[1], p[0], rel, err)
		}
	}
}

// linkedOut opens a root at a new folder holding app.toml ("ours") and the folder sub, then turns
// sub into a link, by linkFolder, to a folder outside the root holding app.toml
// ("theirs"), and reads theirs through the link by path, so a link that led nowhere fails here and
// not as a refusal the rows want. It answers the root, its folder, the outside folder and the outside file's mtime.
func linkedOut(t *testing.T) (root *os.Root, dir, out string, was time.Time) {
	t.Helper()
	dir, out = t.TempDir(), t.TempDir()
	put0(t, filepath.Join(dir, "app.toml"), "ours")
	put0(t, filepath.Join(out, "app.toml"), "theirs")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	root = openRoot(t, dir)
	if err := root.Remove("sub"); err != nil {
		t.Fatal(err)
	}
	if err := linkFolder(t, root, out, "sub"); err != nil {
		t.Fatal(err)
	}
	holds(t, filepath.Join(dir, "sub", "app.toml"), "theirs") // the link leads out, read by path, before any refusal is asked for
	fi, err := os.Stat(filepath.Join(out, "app.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return root, dir, out, fi.ModTime()
}

// untouched fails the test unless out holds app.toml alone, with its bytes and mtime of before.
func untouched(t *testing.T, out string, was time.Time) {
	t.Helper()
	holds(t, filepath.Join(out, "app.toml"), "theirs")
	if fi, err := os.Stat(filepath.Join(out, "app.toml")); err != nil || !fi.ModTime().Equal(was) {
		t.Errorf("the file outside the root is %v, %v, and want its mtime %v", fi, err, was)
	}
	alone(t, out, "app.toml")
}

// KeepIn refuses a folder on the way turned into a link out of its root, whether the link is on
// the way to the file or to the copy, through one root and through two, the link under from or
// under to: nothing outside is read into a copy, and nothing is made there or in the other root.
//
// What it costs when this fails: a program that keeps a copy by names beneath a root another
// account can change copies a file from outside the root, which PutBack later writes in, or puts
// its copy outside.
func TestKeepInRefusesAFolderLinkedOutOfItsRoot(t *testing.T) {
	t.Parallel()
	root, dir, out, was := linkedOut(t)
	other := t.TempDir()
	put0(t, filepath.Join(other, "app.toml"), "ours too")
	or := openRoot(t, other)
	sub := filepath.Join("sub", "app.toml")
	rows := []struct {
		what string
		from *os.Root
		file string
		to   *os.Root
		kept string
	}{
		{"to the file, one root", root, sub, root, "app.toml.v1"},
		{"to the copy, one root", root, "app.toml", root, sub + ".v1"},
		{"to the file, under from, two roots", root, sub, or, "app.toml.v1"},
		{"to the copy, under to, two roots", or, "app.toml", root, sub + ".v1"},
	}
	for _, r := range rows {
		if err := KeepIn(r.from, r.file, r.to, r.kept); err == nil {
			t.Errorf("KeepIn, a folder linked out %s: it made the copy", r.what)
		}
	}
	untouched(t, out, was)
	alone(t, dir, "app.toml", "sub")
	alone(t, other, "app.toml")
}

// PutBackIn refuses a folder on the way turned into a link out of its root, whether the link is on
// the way to the copy or to the file, through one root and through two, the link under from or
// under to: nothing outside is read, hashed or written over, and the file in the other root is left.
//
// What it costs when this fails: a restore by names beneath a root another account can change
// puts a file from outside the root in, or writes over a file outside it.
func TestPutBackInRefusesAFolderLinkedOutOfItsRoot(t *testing.T) {
	t.Parallel()
	root, dir, out, was := linkedOut(t)
	put0(t, filepath.Join(dir, "app.toml.v1"), "kept")
	other := t.TempDir()
	put0(t, filepath.Join(other, "app.toml"), "ours too")
	put0(t, filepath.Join(other, "app.toml.v1"), "kept too")
	or := openRoot(t, other)
	sub := filepath.Join("sub", "app.toml")
	rows := []struct {
		what  string
		from  *os.Root
		kept  string
		to    *os.Root
		file  string
		holds string
	}{
		{"to the copy, one root", root, sub, root, "app.toml", "ours"},
		{"to the file, one root", root, "app.toml.v1", root, sub, "theirs"},
		{"to the copy, under from, two roots", root, sub, or, "app.toml", "ours too"},
		{"to the file, under to, two roots", or, "app.toml.v1", root, sub, "theirs"},
	}
	for _, r := range rows {
		if err := PutBackIn(r.from, r.kept, r.to, r.file, sha256.Sum256([]byte(r.holds))); err == nil {
			t.Errorf("PutBackIn, a folder linked out %s: it put the copy back", r.what)
		}
	}
	holds(t, filepath.Join(dir, "app.toml"), "ours")
	holds(t, filepath.Join(other, "app.toml"), "ours too")
	untouched(t, out, was)
	alone(t, dir, "app.toml", "app.toml.v1", "sub")
	alone(t, other, "app.toml", "app.toml.v1")
}

// KeepIn copies a file to a name in another folder, with its mode, and PutBackIn restores it only
// over the bytes the caller names, with Keep's and PutBack's refusals: a taken name or a link at
// the copy's name is fs.ErrExist, a name that is not plain is refused, no folder is made, a missing
// file makes no copy, a link at kept or file is refused and not as ErrChanged, and a file changed or
// gone is ErrChanged. Each error names its name beneath its root. Each row runs through one root
// over both folders and through two roots, one opened at each of two folders apart.
//
// What it costs when this fails: the root's forms keep or restore what the path forms refuse, or
// make a folder the caller did not ask for.
func TestKeepInThenPutBackInRestoresAFileBeneathTheRoot(t *testing.T) {
	t.Parallel()
	for _, two := range []bool{false, true} {
		name := map[bool]string{false: "one root", true: "two roots"}[two]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fdir, away := t.TempDir(), t.TempDir()
			tdir := fdir
			if two {
				tdir = t.TempDir()
				apart(t, fdir, tdir)
			}
			if err := os.Mkdir(filepath.Join(fdir, "a"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(tdir, "b"), 0o700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(fdir, "a", "app.toml")
			put0(t, file, "before")
			if err := os.Chmod(file, 0o640); err != nil {
				t.Fatal(err)
			}
			from := openRoot(t, fdir)
			to := from
			if two {
				to = openRoot(t, tdir)
			}
			a, kept := filepath.Join("a", "app.toml"), filepath.Join("b", "app.toml.v1")
			if err := KeepIn(from, a, to, kept); err != nil {
				t.Fatal(err)
			}
			holds(t, filepath.Join(tdir, kept), "before")
			permIs(t, filepath.Join(tdir, kept), 0o640)
			alink, blink := filepath.Join("a", "link"), filepath.Join("b", "link")
			if err := linkFolder(t, from, away, alink); err != nil {
				t.Fatal(err)
			}
			if err := linkFolder(t, to, away, blink); err != nil {
				t.Fatal(err)
			}
			for _, at := range []string{kept, blink} {
				if err := KeepIn(from, a, to, at); !errors.Is(err, fs.ErrExist) || !strings.Contains(err.Error(), at+" beneath "+to.Name()) {
					t.Errorf("KeepIn at the taken name %s: %v, and want fs.ErrExist, naming it beneath its root", at, err)
				}
			}
			for _, at := range []string{filepath.Join("c", "app.toml.v1"), filepath.Join("b", "app.toml.v2."), filepath.Join("b", "..")} {
				if err := KeepIn(from, a, to, at); err == nil || errors.Is(err, fs.ErrExist) {
					t.Errorf("KeepIn at %s: %v, and want a refusal of the name", at, err)
				}
			}
			if err := KeepIn(from, filepath.Join("a", "gone"), to, filepath.Join("b", "gone.v1")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("KeepIn of a missing file: %v, and want fs.ErrNotExist", err)
			}
			if err := KeepIn(from, alink, to, filepath.Join("b", "link.v1")); err == nil {
				t.Error("KeepIn copied a link")
			}
			if err := File(file, []byte("after")); err != nil {
				t.Fatal(err)
			}
			after := sha256.Sum256([]byte("after"))
			if err := PutBackIn(to, kept, from, a, sha256.Sum256([]byte("other"))); !errors.Is(err, ErrChanged) || !strings.Contains(err.Error(), a+" beneath "+from.Name()) {
				t.Errorf("PutBackIn over a changed file: %v, and want ErrChanged, naming the file beneath its root", err)
			}
			if err := PutBackIn(to, kept, from, filepath.Join("a", "gone"), after); !errors.Is(err, ErrChanged) {
				t.Errorf("PutBackIn over a missing file: %v, and want ErrChanged", err)
			}
			for _, pair := range [][2]string{{blink, a}, {kept, alink}} {
				if err := PutBackIn(to, pair[0], from, pair[1], after); err == nil || errors.Is(err, ErrChanged) {
					t.Errorf("PutBackIn of %s over %s, a link: %v, and want a refusal that is not ErrChanged", pair[0], pair[1], err)
				}
			}
			holds(t, file, "after")
			if err := PutBackIn(to, kept, from, a, after); err != nil {
				t.Fatal(err)
			}
			holds(t, file, "before")
			permIs(t, file, 0o640)
			alone(t, filepath.Join(fdir, "a"), "app.toml", "link")
			alone(t, filepath.Join(tdir, "b"), "app.toml.v1", "link")
			if two {
				alone(t, fdir, "a")
				alone(t, tdir, "b")
			} else {
				alone(t, fdir, "a", "b")
			}
			alone(t, away)
		})
	}
}

// KeepIn makes a copy of a file beneath one root at a name beneath another, the two folders apart,
// with the file's mode, and PutBackIn puts it back the other way with the copy's mode: a file made
// again 0644 since, put back over its bytes, is 0600 again (on Windows the owner-only list, as
// Keep's other Windows rows have it).
//
// What it costs when this fails: a caller that keeps its copies in a private folder elsewhere on
// the machine cannot use the root's forms, and keeps them by bytes alone, losing the mode.
func TestKeepInAndPutBackInGoBetweenTwoRootsThatShareNoFolder(t *testing.T) {
	t.Parallel()
	adir, bdir := t.TempDir(), t.TempDir()
	apart(t, adir, bdir)
	file := filepath.Join(adir, "app.toml")
	put0(t, file, "before")
	ra, rb := openRoot(t, adir), openRoot(t, bdir)
	if err := KeepIn(ra, "app.toml", rb, "app.toml.v1"); err != nil {
		t.Fatal(err)
	}
	holds(t, filepath.Join(bdir, "app.toml.v1"), "before")
	keptAs(t, filepath.Join(bdir, "app.toml.v1"), 0o600)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := File(file, []byte("after"), Mode(0o644)); err != nil {
		t.Fatal(err)
	}
	if err := PutBackIn(rb, "app.toml.v1", ra, "app.toml", sha256.Sum256([]byte("after"))); err != nil {
		t.Fatal(err)
	}
	holds(t, file, "before")
	keptAs(t, file, 0o600)
	alone(t, adir, "app.toml")
	alone(t, bdir, "app.toml.v1")
}

// With Gone, PutBackIn puts a copy back where the file was removed, holds zero, with the copy's
// mode: a 0400 file kept, removed and put back is 0400, a mode no write gives a new file of
// its own (on Windows the owner-only list and the read-only attribute, which only the copy's mode
// sets). Without Gone a file that is not there is ErrChanged in PutBackIn and PutBack, with its sum
// or with a zero holds, and nothing is made.
//
// What it costs when this fails: a roll-back that restores a file a run removed brings it back
// with a new file's mode, shared, whatever it was.
func TestPutBackInWithGoneRestoresARemovedFileWithTheCopysMode(t *testing.T) {
	t.Parallel()
	adir, bdir := t.TempDir(), t.TempDir()
	file := filepath.Join(adir, "app.toml")
	put0(t, file, "before")
	if err := os.Chmod(file, 0o400); err != nil {
		t.Fatal(err)
	}
	removed := func() { // made writable first, as Windows removes no read-only file
		t.Helper()
		if err := os.Chmod(file, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
	}
	ra, rb := openRoot(t, adir), openRoot(t, bdir)
	if err := KeepIn(ra, "app.toml", rb, "app.toml.v1"); err != nil {
		t.Fatal(err)
	}
	keptAs(t, filepath.Join(bdir, "app.toml.v1"), 0o400)
	removed()
	for _, sum := range [][sha256.Size]byte{sha256.Sum256([]byte("before")), {}} {
		if err := PutBackIn(rb, "app.toml.v1", ra, "app.toml", sum); !errors.Is(err, ErrChanged) {
			t.Errorf("PutBackIn over a removed file, holds %x, without Gone: %v, and want ErrChanged", sum[:4], err)
		}
		if err := PutBack(filepath.Join(bdir, "app.toml.v1"), file, sum); !errors.Is(err, ErrChanged) {
			t.Errorf("PutBack over a removed file, holds %x, without Gone: %v, and want ErrChanged", sum[:4], err)
		}
	}
	alone(t, adir)
	if err := PutBackIn(rb, "app.toml.v1", ra, "app.toml", [sha256.Size]byte{}, Gone()); err != nil {
		t.Fatalf("PutBackIn with Gone over a removed file: %v", err)
	}
	holds(t, file, "before")
	keptAs(t, file, 0o400)
	alone(t, adir, "app.toml")
	alone(t, bdir, "app.toml.v1")
}

// Gone puts a copy back only where nothing is: a file, a folder or a link at the name, the link's
// target outside the root, is ErrChanged in PutBackIn, and nothing there or through it is written.
// A holds that is not zero with Gone is refused, matching fs.ErrInvalid and not ErrChanged, and
// makes nothing. Gone given to PutBack, where nothing is at the name, File, FileFrom, FileIn, Keep,
// KeepIn or Swap is refused, matching fs.ErrInvalid, and nothing is written.
//
// What it costs when this fails: a restore meant for a removed file writes over a file put there
// since, or through a link out of the root; or a caller that passed a sum and Gone, unsure which it
// meant, has one silently dropped; or Gone given to a write it does not change is taken as if it
// did, PutBack's path form among them, which no caller asked to restore a removed file.
func TestGoneRefusesAnythingAtTheNameAndEveryOtherWrite(t *testing.T) {
	t.Parallel()
	adir, bdir, out := t.TempDir(), t.TempDir(), t.TempDir()
	put0(t, filepath.Join(bdir, "app.toml.v1"), "kept")
	put0(t, filepath.Join(adir, "there"), "there")
	if err := os.Mkdir(filepath.Join(adir, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	put0(t, filepath.Join(out, "app.toml"), "theirs")
	plantLink(t, filepath.Join(out, "app.toml"), filepath.Join(adir, "link"))
	ra, rb := openRoot(t, adir), openRoot(t, bdir)
	kept, zero := filepath.Join(bdir, "app.toml.v1"), [sha256.Size]byte{}
	for _, at := range []string{"there", "folder", "link"} {
		if err := PutBackIn(rb, "app.toml.v1", ra, at, zero, Gone()); !errors.Is(err, ErrChanged) {
			t.Errorf("PutBackIn with Gone over %s: %v, and want ErrChanged", at, err)
		}
	}
	holds(t, filepath.Join(adir, "there"), "there")
	holds(t, filepath.Join(out, "app.toml"), "theirs")
	alone(t, filepath.Join(adir, "folder"))
	sum := sha256.Sum256([]byte("there"))
	invalid := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, fs.ErrInvalid) || errors.Is(err, ErrChanged) {
			t.Errorf("%s: %v, and want a refusal matching fs.ErrInvalid and not ErrChanged", what, err)
		}
	}
	invalid("PutBackIn with Gone and a sum", PutBackIn(rb, "app.toml.v1", ra, "new", sum, Gone()))
	invalid("PutBack with Gone, where nothing is", PutBack(kept, filepath.Join(adir, "new"), zero, Gone()))
	invalid("File with Gone", File(filepath.Join(adir, "new"), []byte("x"), Gone()))
	invalid("FileFrom with Gone", FileFrom(filepath.Join(adir, "new"), bytes.NewReader([]byte("x")), Gone()))
	invalid("FileIn with Gone", FileIn(ra, "new", []byte("x"), Gone()))
	_, err := Keep(filepath.Join(adir, "there"), adir, "new", Gone())
	invalid("Keep with Gone", err)
	invalid("KeepIn with Gone", KeepIn(ra, "there", ra, "new", Gone()))
	put0(t, filepath.Join(adir, "fresh"), "fresh")
	invalid("Swap with Gone", Swap(filepath.Join(adir, "there"), filepath.Join(adir, "fresh"), filepath.Join(adir, "aside"), Gone()))
	holds(t, filepath.Join(adir, "there"), "there")
	alone(t, adir, "folder", "fresh", "link", "there")
	alone(t, bdir, "app.toml.v1")
	alone(t, out, "app.toml")
}

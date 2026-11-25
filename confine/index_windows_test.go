package confine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/internal/fileid"
)

// Each row makes the seam read a normal volume as one that gives a file no index, then drives one of
// confine's compares-by-file-ID and requires fileid.ErrNoFileIndex, one value with write.ErrNoFileIndex,
// so errors.Is holds with it, in place of the "same file" or "below" answer it would otherwise give.
// Removing the guard call by hand turns each red.
//
// What it costs when this fails: on a WebDAV or SMB drive or on ReFS, where os.SameFile calls any two
// files the same, a file or folder swapped in at a name is taken for the one confine looked at.

func wantNoIndex(t *testing.T, err error, what string) {
	t.Helper()
	if !errors.Is(err, fileid.ErrNoFileIndex) || !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("%s on a volume that gives a file no index: %v, want fileid.ErrNoFileIndex (matching ErrUnsupported)", what, err)
	}
}

func TestWalkNextRefusesANoIndexFolder(t *testing.T) { //nolint:paralleltest // serial: SeamNoIndex moves shared state in internal/fileid
	// serial: SeamNoIndex moves shared state in internal/fileid
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	look, err := root.Stat("sub")
	if err != nil {
		t.Fatal(err)
	}
	defer fileid.SeamNoIndex()()
	sub, err := next(root, "sub", look, "sub")
	if sub != nil {
		_ = sub.Close()
	}
	wantNoIndex(t, err, "next (indexRoot)")
}

func TestCheckOpenedRefusesANoIndexFile(t *testing.T) { //nolint:paralleltest // serial: SeamNoIndex moves shared state in internal/fileid
	// serial: SeamNoIndex moves shared state in internal/fileid
	path := filepath.Join(t.TempDir(), "x")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	walked, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	defer fileid.SeamNoIndex()()
	wantNoIndex(t, checkOpened(f, "x", walked, os.O_RDWR), "checkOpened (indexHeld)")
}

func TestCollideRefusesANoIndexVolumeWhenBothNamesExist(t *testing.T) { //nolint:paralleltest // serial: SeamNoIndex moves shared state in internal/fileid
	// serial: SeamNoIndex moves shared state in internal/fileid
	dir := t.TempDir()
	for _, n := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	defer fileid.SeamNoIndex()()
	_, err = collide(root, "a", "b", func() {})
	wantNoIndex(t, err, "collide (indexSame)")
}

// The guard of collide's compare of two names that are both there reads each name itself, as the
// compare's Lstat does, and follows no link: a junction that leads out of the folder is read as the
// junction, so Collide answers and does not refuse it, and with the seam the junction is what is
// refused. Opening the name through the root in indexSame, which follows a link that stays inside
// and refuses one that leads out, turns the first half red.
//
// What it costs when this fails: the index judged is of a link's target, a file the compare never
// looks at, so a no-index link passes for a known file, or a link that leads out makes Collide fail.
func TestCollideReadsTheIndexOfALinkItself(t *testing.T) { //nolint:paralleltest // serial: SeamNoIndex moves shared state in internal/fileid
	// serial: SeamNoIndex moves shared state in internal/fileid
	dir, out := t.TempDir(), t.TempDir()
	plantJunction(t, out, filepath.Join(dir, "j"))
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	if same, err := collide(root, "j", "f", func() {}); err != nil || same {
		t.Errorf("collide of a junction that leads out and a file: %v, %v, want false and no error", same, err)
	}
	defer fileid.SeamNoIndex()()
	_, err = collide(root, "j", "f", func() {})
	wantNoIndex(t, err, "collide of a junction that leads out (indexSame)")
}

// What it costs when this fails: a file put at the name after collide's look has its index judged
// in place of the one compared, so a no-index file compared passes on a known file's index.
func TestIndexSameRefusesANameChangedSinceItsLook(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, n := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	other, err := root.Lstat("b")
	if err != nil {
		t.Fatal(err)
	}
	var refused *fs.PathError
	if err := indexSame(root, "a", other); !errors.As(err, &refused) || refused.Op != opCollide || refused.Path != "a" {
		t.Errorf("indexSame of a name whose look is of another file: %v, want an *fs.PathError of collide naming a", err)
	}
	mine, err := root.Lstat("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := indexSame(root, "a", mine); err != nil {
		t.Errorf("indexSame of a name with its own look: %v, want nil", err)
	}
}

func TestBelowRefusesANoIndexVolume(t *testing.T) { //nolint:paralleltest // serial: SeamNoIndex moves shared state in internal/fileid
	// serial: SeamNoIndex moves shared state in internal/fileid
	dir := t.TempDir()
	defer fileid.SeamNoIndex()()
	_, err := Below(dir, dir)
	wantNoIndex(t, err, "Below (indexAtPath)")
}

func TestResolveCheckedRefusesANoIndexVolume(t *testing.T) { //nolint:paralleltest // serial: SeamNoIndex moves shared state in internal/fileid
	// serial: SeamNoIndex moves shared state in internal/fileid
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer fileid.SeamNoIndex()()
	_, _, err := resolveChecked(path, func() {})
	wantNoIndex(t, err, "resolveChecked (indexAtPath)")
}

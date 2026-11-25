package confine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// looksOne is what the system's own look says of a and b in dir: a made in a fresh folder there,
// then looked for as b.
func looksOne(t *testing.T, dir, a, b string) bool {
	t.Helper()
	sub, err := os.MkdirTemp(dir, "look")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sub) })
	if err := os.WriteFile(filepath.Join(sub, a), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = os.Lstat(filepath.Join(sub, b))
	return err == nil
}

// Folds answers as the system's own look reads names, for case and for é composed and decomposed,
// in each folder this runner can make: the temp folder, and on each system the folders its own
// file names (a casefold folder on Linux, a case-sensitive one on Windows, a tmpfs folder) where
// the runner lets one be made. Where it answers FoldUnknown, the error matches ErrNoFolding.
//
// What it costs when this fails: a product that keeps two names apart because Folds said the
// folder tells them apart writes both, and they are one file on disk.
func TestFoldsAnswersAsTheFolderReadsNames(t *testing.T) {
	t.Parallel()
	for _, dir := range append([]string{t.TempDir()}, foldsFolders(t)...) {
		got, err := confine.Folds(dir)
		t.Logf("Folds(%s) = %v, %v", dir, got, err)
		if got == confine.FoldUnknown {
			if !errors.Is(err, confine.ErrNoFolding) || !errors.Is(err, errors.ErrUnsupported) {
				t.Errorf("Folds(%s) = %v, %v, want ErrNoFolding with FoldUnknown", dir, got, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("Folds(%s) = %v, %v, want no error with an answer", dir, got, err)
		}
		if want := looksOne(t, dir, "Fold", "fold"); got.FoldsCase() != want {
			t.Errorf("Folds(%s) = %v, FoldsCase %v, and the system's own look reads Fold as fold: %v", dir, got, got.FoldsCase(), want)
		}
		if want := looksOne(t, dir, "é", "é"); got.FoldsComposition() != want {
			t.Errorf("Folds(%s) = %v, FoldsComposition %v, and the system's own look reads é composed as decomposed: %v", dir, got, got.FoldsComposition(), want)
		}
	}
}

// Folds writes nothing in the folder: what it holds and its modification time are as they were,
// and on Unix it answers a folder this account may not write as it answers one it may.
//
// What it costs when this fails: a caller that only asks is refused on a read-only folder, or two
// callers asking at once see each other's probes, as with Collide.
func TestFoldsWritesNothingInTheFolder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kept"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, beforeNames := folderState(t, dir)
	wantGot, wantErr := confine.Folds(dir)
	after, afterNames := folderState(t, dir)
	if !after.ModTime().Equal(before.ModTime()) || !slices.Equal(beforeNames, afterNames) {
		t.Errorf("after Folds the folder holds %v, changed %v, and before %v, changed %v", afterNames, after.ModTime(), beforeNames, before.ModTime())
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	if got, err := confine.Folds(dir); got != wantGot || (err == nil) != (wantErr == nil) {
		t.Errorf("Folds of the folder read-only = %v, %v, and writable %v, %v", got, err, wantGot, wantErr)
	}
}

// folderState is dir's own facts and the names in it.
func folderState(t *testing.T, dir string) (fs.FileInfo, []string) {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return info, names
}

// A root that is not a folder is refused with FoldUnknown: a file with an *fs.PathError matching
// fs.ErrInvalid, and a missing name with fs.ErrNotExist.
//
// What it costs when this fails: a file's attributes are read as its folder's, and a folder that
// folds is answered by a file in it that never does.
func TestFoldsRefusesARootThatIsNotAFolder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := confine.Folds(file)
	if pe, ok := errors.AsType[*fs.PathError](err); !ok || !errors.Is(err, fs.ErrInvalid) || pe.Path != file || got != confine.FoldUnknown {
		t.Errorf("Folds(a file) = %v, %v, want FoldUnknown and an *fs.PathError naming it matching fs.ErrInvalid", got, err)
	}
	if got, err := confine.Folds(filepath.Join(dir, "missing")); !errors.Is(err, fs.ErrNotExist) || got != confine.FoldUnknown {
		t.Errorf("Folds(a missing name) = %v, %v, want FoldUnknown and fs.ErrNotExist", got, err)
	}
}

// FoldUnknown reads as folding both case and composition, each answer reads as its own bits, and
// each has its name.
//
// What it costs when this fails: a caller that asks FoldsCase of FoldUnknown is told no, and keeps
// apart two names that may be one file.
func TestFoldUnknownReadsAsFoldingBoth(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		f               confine.Folding
		cased, composed bool
		name            string
	}{
		{confine.FoldNone, false, false, "none"},
		{confine.FoldCase, true, false, "case"},
		{confine.FoldComposition, false, true, "composition"},
		{confine.FoldBoth, true, true, "both"},
		{confine.FoldUnknown, true, true, "unknown"},
		{confine.Folding(9), true, false, "Folding(9)"},
	} {
		if c.f.FoldsCase() != c.cased || c.f.FoldsComposition() != c.composed || c.f.String() != c.name {
			t.Errorf("%d: FoldsCase %v, FoldsComposition %v, String %q, want %v, %v, %q", uint8(c.f), c.f.FoldsCase(), c.f.FoldsComposition(), c.f.String(), c.cased, c.composed, c.name)
		}
	}
}

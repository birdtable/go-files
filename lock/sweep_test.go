package lock_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/lock"
)

// scratch is a program's folder with a scratch base in it, and the lock beside the base.
func scratch(t *testing.T) (path, base string) {
	t.Helper()
	dir := t.TempDir()
	base = filepath.Join(dir, "scratch")
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "scratch.lock"), base
}

// run makes a run's folder beneath base, with a file in it, as a run that stopped leaves it.
func run(t *testing.T, base string) string {
	t.Helper()
	dir, err := os.MkdirTemp(base, "run-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "out"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func there(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

// What it costs when this fails: the sweep removes the folder of a run that is still in it, and
// the run's output is gone mid-run.
func TestTheSweepLeavesEveryFolderWhileARunHoldsTheLock(t *testing.T) {
	t.Parallel()
	path, base := scratch(t)
	dir := run(t, base)
	l, err := lock.Take(path, "") // a run in its folder
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	err = sweep(path, base, func(name string) { t.Errorf("removed %s while a run holds the lock", name) })
	if !lock.IsHeld(err) {
		t.Errorf("sweep: %v, and want Held", err)
	}
	if !there(t, filepath.Join(dir, "out")) {
		t.Errorf("%s was removed while a run holds the lock", dir)
	}
}

// What it costs when this fails: a run starts in the scratch base while the sweep is removing
// it, and its folder or its files go with the ones the sweep meant.
func TestARunCannotStartWhileTheSweepRemoves(t *testing.T) {
	t.Parallel()
	path, base := scratch(t)
	run(t, base)
	run(t, base)
	n := 0
	err := sweep(path, base, func(string) {
		n++
		l, err := lock.Take(path, "") // a run that starts mid-sweep
		if !lock.IsHeld(err) {
			t.Errorf("a run took the lock after %d of the sweep's removals: %v", n, err)
			l.Release()
		}
	})
	if err != nil || n != 2 {
		t.Errorf("sweep: %v, %d removed, and want nil, 2", err, n)
	}
}

// What it costs when this fails: the sweep removes what is not a run's, in the base, or outside
// it through a link.
func TestTheSweepRemovesTheRunsFoldersAloneAndNeverThroughALink(t *testing.T) {
	t.Parallel()
	path, base := scratch(t)
	outside := t.TempDir()
	kept := filepath.Join(outside, "kept")
	if err := os.WriteFile(kept, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := run(t, base)
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil { // developer mode on Windows
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "run-link")); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(base, "other")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := sweep(path, base, func(name string) { got = append(got, name) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || there(t, dir) || there(t, filepath.Join(base, "run-link")) {
		t.Errorf("removed %v, and want the run's folder and the run-link link, both gone", got)
	}
	if !there(t, kept) || !there(t, other) {
		t.Errorf("%s or %s is gone: the sweep removed what was not a run's", kept, other)
	}
}

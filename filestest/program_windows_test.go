package filestest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/filestest"
)

// exeSuffix ends the name of a program the tests write.
const exeSuffix = ".exe"

// tempDirRefused is whether trust.Path refuses t.TempDir's folder as a private folder: a default
// profile's temp folder holds this account, SYSTEM and Administrators alone, so it may pass.
const tempDirRefused = false

// program is a program that exits 0, its arguments, and how many goroutines write and start it
// how many times each: this test binary, started so it exits at once, too large to write many of.
func program(t *testing.T) (content []byte, args []string, goroutines, rounds int) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	return content, []string{exitArg}, 2, 2
}

// A removal of Private's folder that fails fails the test, saying the path.
//
// What it costs when this fails: a test run leaves a private folder behind, and passes saying
// nothing of it.
func TestPrivateFailsTheTestWhoseFolderCannotBeRemoved(t *testing.T) {
	t.Parallel()
	var p string
	r := (&recorder{TB: t}).run(func(r *recorder) {
		p = filestest.Private(r)
		filestest.WriteFile(r, filepath.Join(p, "held"), []byte("x"))
	})
	if r.failed {
		t.Fatalf("Private: %q", r.said)
	}
	held, err := os.Open(filepath.Join(p, "held")) // opened with no FILE_SHARE_DELETE, so it cannot be removed while open
	if err != nil {
		t.Fatal(err)
	}
	r.end()
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(p); err != nil {
		t.Fatal(err)
	}
	if !r.failed || !r.says(p) {
		t.Errorf("Private's %s, which could not be removed: failed %v, said %q, and want a failure saying the path", p, r.failed, r.said)
	}
}

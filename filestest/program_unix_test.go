//go:build unix

package filestest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/filestest"
)

// exeSuffix ends the name of a program the tests write.
const exeSuffix = ""

// tempDirRefused is whether trust.Path refuses t.TempDir's folder as a private folder: it is
// 0777 less the umask, one the group or others may enter.
const tempDirRefused = true

// program is a program that exits 0, its arguments, and how many goroutines write and start it
// how many times each: a shell script, small enough to write many of.
func program(*testing.T) (content []byte, args []string, goroutines, rounds int) {
	return []byte("#!/bin/sh\nexit 0\n"), nil, 4, 8
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
	if err := os.Chmod(p, 0o500); err != nil {
		t.Fatal(err)
	}
	r.end()
	if err := os.Chmod(p, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(p); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Logf("root removes a folder it may not write: %q", r.said)
		return
	}
	if !r.failed || !r.says(p) {
		t.Errorf("Private's %s, which could not be removed: failed %v, said %q, and want a failure saying the path", p, r.failed, r.said)
	}
}

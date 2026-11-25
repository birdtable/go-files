package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests read the module's own tree and trees they write in t.TempDir. The package's TestMain
// does not run testkit.Main: what internal/cmd/measure imports is the standard library alone.

// moduleRoot is the module's root, three folders up from this package's.
func moduleRoot() string { return filepath.Join("..", "..", "..") }

// tree writes files, each a path from the root and its text, into a new folder and gives it.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// lines gives n lines of Go comment, each ended by a newline.
func lines(n int) string { return strings.Repeat("// a line\n", n) }

// What it costs when this fails: the size a tag is held to counts tests, test support or test
// data, or misses a system's file or the Windows calls the packages share, and a tag passes or
// fails on lines no program links, or on fewer than a program links.
func TestSizeCountsTheNonTestLinesAndRefusesAboveTheCeiling(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"a/a.go":                            lines(1000),
		"a/b/b_windows.go":                  lines(Ceiling - 1100),
		"internal/winapi/w_windows.go":      lines(100),
		"internal/winapi/w_windows_test.go": lines(5000),
		"internal/w.go":                     lines(5000),
		"a/a_test.go":                       lines(5000),
		"a/testdata/t.go":                   lines(5000),
		"internal/x/x.go":                   lines(5000),
		".hidden/h.go":                      lines(5000),
		"treesum/s.go":                      lines(ApartCeiling - 50),
		"treesum/s_unix.go":                 lines(50),
		"treesum/s_test.go":                 lines(5000),
		"a/notes.txt":                       lines(5000),
	}
	dir := tree(t, files)
	var out bytes.Buffer
	if code := run([]string{"size"}, dir, &out); code != 0 {
		t.Errorf("size exited %d at the ceiling, and want 0: %s", code, &out)
	}
	if want := fmt.Sprintf("non-test Go lines outside internal/ but internal/winapi/, and outside treesum/: %d (the ceiling is %d)\n", Ceiling, Ceiling) +
		fmt.Sprintf("non-test Go lines of treesum/: %d (its ceiling is %d)\n", ApartCeiling, ApartCeiling); out.String() != want {
		t.Errorf("size said %q, and want %q", &out, want)
	}
	for _, c := range []struct{ file, want string }{
		{"a/a.go", fmt.Sprintf("%d lines is 1 above the ceiling", Ceiling+1)},
		{"internal/winapi/w_windows.go", fmt.Sprintf("%d lines is 1 above the ceiling", Ceiling+1)},
		{"treesum/s_unix.go", fmt.Sprintf("treesum/: %d lines is 1 above its ceiling", ApartCeiling+1)},
	} {
		dir := tree(t, files)
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(c.file)))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(c.file)), append(b, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		out.Reset()
		if code := run([]string{"size"}, dir, &out); code != 1 {
			t.Errorf("size exited %d with a line added to %s, and want 1: %s", code, c.file, &out)
		}
		if !strings.Contains(out.String(), c.want) {
			t.Errorf("size did not say %q:\n%s", c.want, &out)
		}
	}
}

// What it costs when this fails: a task run with no word, a misspelt one or one measure no longer
// has (api and api-check went with API.txt) passes and checks nothing.
func TestRunTakesOneWordItKnows(t *testing.T) {
	t.Parallel()
	dir := tree(t, map[string]string{"a/a.go": "package a\n"})
	for _, args := range [][]string{nil, {"unread", "size"}, {"sise"}, {"api"}, {"api-check"}} {
		var out bytes.Buffer
		if code := run(args, dir, &out); code != 2 {
			t.Errorf("run(%q) exited %d, and want 2: %s", args, code, &out)
		}
	}
}

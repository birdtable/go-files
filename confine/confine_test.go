package confine_test

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// refusedPaths name something outside a root, or nothing a root holds.
func refusedPaths() []string {
	return []string{"..", "../x", "a/../../x", "a/..", "/etc/passwd", "/", "C:/x", "C:\\x", "c:x", "a\\..\\..\\x",
		"a\\b", "a:b", "a//b", "a/./b", "./a", "a/", ".", "\x00", "a\x00b",
		"a.txt/..namedfork/rsrc", "..namedfork", "a/..NAMEDFORK/data", "a/..NamedFork"}
}

// takenPaths are clean relative paths inside a root.
func takenPaths() []string {
	return []string{"a", "a/b.txt", ".app/pin.yaml", "...", "..a", "a..", "a b/c"}
}

// Climbing, absolute, drive-style, a stream, a backslash, an unclean path,
// a NUL and a macOS fork, ..namedfork in any case, are each refused as
// ErrOutside; a clean relative path is taken
// in this system's form, and the empty path is the root.
//
// What it costs when this fails: a path from a flag or a file names
// something outside the folder, or a different file on Windows than on
// Linux.
func TestLocalRefusesWhatLeavesTheRootAndTakesTheRest(t *testing.T) {
	t.Parallel()
	refusedN, takenN := 0, 0
	for _, p := range refusedPaths() {
		if got, err := confine.Local(p); !errors.Is(err, confine.ErrOutside) {
			t.Errorf("Local(%q) = %q, %v, want ErrOutside", p, got, err)
		}
		refusedN++
	}
	for _, p := range takenPaths() {
		if got, err := confine.Local(p); err != nil || got != filepath.FromSlash(p) {
			t.Errorf("Local(%q) = %q, %v, want it taken", p, got, err)
		}
		takenN++
	}
	if refusedN == 0 || takenN == 0 {
		t.Fatalf("checked %d paths to refuse and %d to take", refusedN, takenN)
	}
	if got, err := confine.Local(""); err != nil || got != "." {
		t.Errorf("Local(\"\") = %q, %v, want the root", got, err)
	}
}

// Whatever Local takes is local to the root on this system, holds no
// backslash, colon or NUL, and is the slash path it was given; whatever it
// refuses matches ErrOutside. The seeds in testdata/fuzz/FuzzLocal run as
// tests in every go test.
func FuzzLocal(f *testing.F) {
	for _, p := range append(append(refusedPaths(), takenPaths()...), "", "a/b/../c", "CON", "a/\u2215b") {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, p string) {
		got, err := confine.Local(p)
		if err != nil {
			if !errors.Is(err, confine.ErrOutside) {
				t.Errorf("Local(%q) refused with %v, which is not ErrOutside", p, err)
			}
			return
		}
		if p == "" {
			if got != "." {
				t.Errorf("Local(\"\") = %q, want .", got)
			}
			return
		}
		if !filepath.IsLocal(got) || strings.ContainsAny(got, ":\x00") || strings.ContainsRune(p, '\\') {
			t.Errorf("Local(%q) = %q, which is not local to the root", p, got)
		}
		if filepath.ToSlash(got) != p || filepath.Clean(got) != got {
			t.Errorf("Local(%q) = %q, which is not the path given, clean", p, got)
		}
		for part := range strings.SplitSeq(p, "/") {
			if part == ".." || part == "." || part == "" || strings.EqualFold(part, "..namedfork") {
				t.Errorf("Local(%q) took the element %q", p, part)
			}
		}
	})
}

// refusedAt fails the test unless err matches ErrOutside and holds an
// *fs.PathError naming at, slash-separated.
func refusedAt(t *testing.T, what string, err error, at string) {
	t.Helper()
	if !errors.Is(err, confine.ErrOutside) {
		t.Errorf("%s = %v, want ErrOutside", what, err)
		return
	}
	if pe, ok := errors.AsType[*fs.PathError](err); !ok || pe.Path != at {
		t.Errorf("%s = %v, want an *fs.PathError naming %s", what, err, at)
	}
}

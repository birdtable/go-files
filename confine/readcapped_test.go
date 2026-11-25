package confine_test

import (
	"bytes"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// What it costs when this fails: a program reading a file its repository holds is handed the bytes
// of a file outside it, or inside it by another name, through a link the repository carries, or
// reads a folder's listing as a file's bytes.
func TestReadCappedAnswersTheFileAndRefusesALinkAtAnyComponent(t *testing.T) {
	t.Parallel()
	in, out := t.TempDir(), t.TempDir()
	writeMode(t, filepath.Join(out, "secret"), 0o600)
	writeMode(t, filepath.Join(in, "notes"), 0o644)
	if err := os.Mkdir(filepath.Join(in, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMode(t, filepath.Join(in, "real", "notes"), 0o644)
	plantLink(t, filepath.Join(out, "secret"), filepath.Join(in, "far"))
	plantLink(t, out, filepath.Join(in, "sub"))
	plantLink(t, "notes", filepath.Join(in, "near")) // relative, so it stays inside the root
	plantLink(t, "real", filepath.Join(in, "nearDir"))
	root := openRoot(t, in)
	for _, row := range []struct {
		name string
		want error // nil answers "data"
	}{
		{"notes", nil},
		{filepath.Join("real", "notes"), nil},
		{"gone", fs.ErrNotExist},
		{"real", confine.ErrNotRegular},
		{"far", confine.ErrOutside},
		{filepath.Join("sub", "secret"), confine.ErrOutside},
		{"near", confine.ErrOutside},
		{filepath.Join("nearDir", "notes"), confine.ErrOutside},
		{filepath.Join("..", filepath.Base(out), "secret"), confine.ErrOutside},
		{"real" + string(filepath.Separator) + ".." + string(filepath.Separator) + "notes", confine.ErrOutside}, // not clean
	} {
		got, err := confine.ReadCapped(root, row.name, 4)
		_, isPath := errors.AsType[*fs.PathError](err)
		switch {
		case row.want == nil && (err != nil || string(got) != "data"):
			t.Errorf("%s: %q, %v, and want data", row.name, got, err)
		case row.want != nil && (got != nil || !errors.Is(err, row.want) || !isPath):
			t.Errorf("%s: %q, %v, and want nothing and an *fs.PathError matching %v", row.name, got, err, row.want)
		}
	}
}

// What it costs when this fails: a file one byte over the cap is answered, or a file at the cap is
// refused, or a cap at the largest int64 reads nothing.
func TestReadCappedRefusesOneByteOverTheCap(t *testing.T) {
	t.Parallel()
	in := t.TempDir()
	writeMode(t, filepath.Join(in, "four"), 0o644)
	if err := os.WriteFile(filepath.Join(in, "empty"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, in)
	for _, row := range []struct {
		name  string
		limit int64
		want  string
		err   error
	}{
		{"four", 4, "data", nil},
		{"four", math.MaxInt64, "data", nil},
		{"four", 3, "", confine.ErrOverCap},
		{"empty", 0, "", nil},
		{"four", 0, "", confine.ErrOverCap},
		{"four", -1, "", fs.ErrInvalid},
	} {
		got, err := confine.ReadCapped(root, row.name, row.limit)
		if row.err == nil && (err != nil || !bytes.Equal(got, []byte(row.want))) ||
			row.err != nil && (got != nil || !errors.Is(err, row.err)) {
			t.Errorf("%s capped at %d: %q, %v, and want %q, %v", row.name, row.limit, got, err, row.want, row.err)
		}
	}
}

// What it costs when this fails: a process that swaps a file of the repository for a link to a
// file outside it, or for a folder, between the caller's look and the open, has that file's bytes
// or the folder's read as the repository's file.
func TestReadCappedReadsNothingOutsideWhenTheNameIsSwappedBeforeTheOpen(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		swap string
		want error
	}{
		{"link", confine.ErrOutside},
		{"folder", confine.ErrNotRegular},
	} {
		in, out := t.TempDir(), t.TempDir()
		writeMode(t, filepath.Join(out, "secret"), 0o600)
		writeMode(t, filepath.Join(in, "x"), 0o644)
		swapped := false
		got, err := confine.ReadCappedBeforeOpen(openRoot(t, in), "x", 64, func() {
			if err := os.Remove(filepath.Join(in, "x")); err != nil {
				t.Fatal(err)
			}
			if row.swap == "link" {
				plantLink(t, filepath.Join(out, "secret"), filepath.Join(in, "x"))
			} else if err := os.Mkdir(filepath.Join(in, "x"), 0o755); err != nil {
				t.Fatal(err)
			}
			swapped = true
		})
		if !swapped || got != nil || !errors.Is(err, row.want) {
			t.Errorf("x swapped (%v) for a %s before the open: %q, %v, and want nothing and %v", swapped, row.swap, got, err, row.want)
		}
	}
}

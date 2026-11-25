//go:build unix

package confine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// copyFrom makes a folder holding a.txt 0o644, run 0o755 and sub/b.txt in sub 0o755, and answers it.
func copyFrom(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, mode := range map[string]fs.FileMode{"a.txt": 0o644, "run": 0o755, filepath.Join("sub", "b.txt"): 0o644} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(name), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// CopyTree sets 0o600 on a file, 0o700 on a program and on a folder, exactly, under a umask of
// 077, 277 or 0, whatever the source's modes, and leaves dst's own mode.
//
// What it costs when this fails: a copy of a tree others may read is readable by them too, or a
// program copied under a umask of 277 is left one its owner cannot write or run.
func TestCopyTreeSetsItsModesExactlyUnderAnyUmask(t *testing.T) { //nolint:paralleltest // serial: sets the process umask, which every file a parallel test made would take
	from := copyFrom(t)
	for _, mask := range []int{0o077, 0o277, 0} {
		to := t.TempDir()
		if err := os.Chmod(to, 0o711); err != nil {
			t.Fatal(err)
		}
		old := syscall.Umask(mask)
		err := CopyTree(rootAt(t, to), rootAt(t, from))
		syscall.Umask(old)
		if err != nil {
			t.Fatalf("umask %o: %v", mask, err)
		}
		for name, want := range map[string]fs.FileMode{".": fs.ModeDir | 0o711, "a.txt": 0o600, "run": 0o700, "sub": fs.ModeDir | 0o700, filepath.Join("sub", "b.txt"): 0o600} {
			if info, err := os.Lstat(filepath.Join(to, name)); err != nil || info.Mode() != want {
				t.Errorf("umask %o: %s is %v, %v, and want %v", mask, name, info.Mode(), err, want)
			}
		}
	}
}

// CopyTree refuses a pipe in src, and a file swapped for a pipe between its look and its open,
// matching fs.ErrInvalid and naming it, and makes nothing at its name in the copy.
//
// What it costs when this fails: a pipe's or a device's stream is copied into the tree as a file,
// empty or endless, in place of what the source held.
func TestCopyTreeRefusesAPipeAndAFileSwappedForOneBeforeItsOpen(t *testing.T) {
	t.Parallel()
	for name, swap := range map[string]bool{"a pipe": false, "swapped before the open": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			from := copyFrom(t)
			pipe := func() {
				if err := os.Remove(filepath.Join(from, "a.txt")); err != nil {
					t.Fatal(err)
				}
				if err := syscall.Mkfifo(filepath.Join(from, "a.txt"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if !swap {
				pipe()
			}
			to := t.TempDir()
			err := copyTree(rootAt(t, to), rootAt(t, from), func(step string) {
				if swap && step == stepCopyLooked {
					swap = false
					pipe()
				}
			})
			if !errors.Is(err, fs.ErrInvalid) {
				t.Fatalf("CopyTree = %v, and want it to match fs.ErrInvalid", err)
			}
			if pe, ok := errors.AsType[*fs.PathError](err); !ok || pe.Path != "a.txt" {
				t.Errorf("CopyTree = %v, and want an *fs.PathError naming a.txt", err)
			}
			if _, err := os.Lstat(filepath.Join(to, "a.txt")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("a.txt is in the copy (%v), and want nothing made", err)
			}
		})
	}
}

// CopyTree refuses a root whose Name names another folder than it holds, its folder moved away
// and another made at the name, matching ErrOutside, before anything is made in either.
//
// What it costs when this fails: a link of the copy names its target under a path that is now
// another folder's, so it leads out of the copy.
func TestCopyTreeRefusesARootWhoseNameNamesAnotherFolder(t *testing.T) {
	t.Parallel()
	for _, moved := range []string{"dst", "src"} {
		t.Run(moved, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			paths := map[string]string{"dst": filepath.Join(dir, "dst"), "src": copyFrom(t)}
			if err := os.Mkdir(paths["dst"], 0o700); err != nil {
				t.Fatal(err)
			}
			dst, src := rootAt(t, paths["dst"]), rootAt(t, paths["src"])
			if err := os.Rename(paths[moved], filepath.Join(dir, "away")); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(paths[moved], 0o700); err != nil {
				t.Fatal(err)
			}
			err := CopyTree(dst, src)
			if !errors.Is(err, ErrOutside) {
				t.Fatalf("CopyTree = %v, and want it to match ErrOutside", err)
			}
			if pe, ok := errors.AsType[*fs.PathError](err); !ok || pe.Path != paths[moved] {
				t.Errorf("CopyTree = %v, and want an *fs.PathError naming %s", err, paths[moved])
			}
			held := paths["dst"]
			if moved == "dst" {
				held = filepath.Join(dir, "away")
			}
			for _, d := range []string{paths[moved], held} {
				if names := namesIn(d); len(names) != 0 {
					t.Errorf("%s holds %q, and want nothing made", d, names)
				}
			}
		})
	}
}

//go:build linux || darwin

package confine

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// kernelOpens is true: open is the kernel's, which refuses a link wherever it meets one.
const kernelOpens = true

// plain is a folder holding a.txt and the empty folder sub, opened as a root.
func plain(t *testing.T) *os.Root {
	t.Helper()
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644))
	must(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))
	root, err := os.OpenRoot(dir)
	must(t, err)
	t.Cleanup(func() { _ = root.Close() })
	return root
}

// swapped is a folder holding d/f.txt, opened as a root, and the hook open
// calls at each step: before the open it moves d to e and puts a link d -> e
// in its place, so d/f.txt is the very file the walk found, reached through
// a link.
func swapped(t *testing.T) (*os.Root, string, func(string)) {
	t.Helper()
	root, dir, hook := raced(t, map[string]func(string){stepOpen: func(dir string) {
		must(t, os.Rename(filepath.Join(dir, "d"), filepath.Join(dir, "e")))
		plantLink(t, "e", filepath.Join(dir, "d"))
	}})
	must(t, os.Mkdir(filepath.Join(dir, "d"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "d", "f.txt"), []byte("f\n"), 0o644))
	return root, dir, hook
}

// Open resolves in the kernel, openat2 on Linux and O_NOFOLLOW_ANY on macOS: a folder swapped for a
// link to itself just before the open is refused, read or written, and the file keeps every byte.
// The walk alone opens the same file from the folder it already holds, now named e: it is the
// file the walk found, and the case shows the kernel is what refuses here.
//
// What it costs when this fails: a process that swaps a folder of the path for a link at the right
// moment has a program read or write through it, where the kernel could have refused it.
func TestTheKernelRefusesAFolderSwappedForALinkJustBeforeTheOpen(t *testing.T) {
	t.Parallel()
	tried := 0
	for how, flag := range map[string]int{"read": os.O_RDONLY, "replace": os.O_WRONLY | os.O_TRUNC} {
		root, dir, hook := swapped(t)
		f, handled, err := sysOpen(kernelOpen).open(root, filepath.Join("d", "f.txt"), flag, 0, hook)
		if !handled {
			t.Fatal("the kernel has no openat2")
		}
		refused(t, "the kernel's open to "+how, f, err)
		holds(t, dir, filepath.Join("e", "f.txt"), "f\n")
		tried++
	}
	if tried != 2 {
		t.Fatalf("tried %d of 2 opens", tried)
	}
	root, _, hook := swapped(t)
	f, err := walkOpen(root, filepath.Join("d", "f.txt"), os.O_RDONLY, 0, hook)
	if err != nil {
		t.Fatalf("the walk alone refused the swap (%v): the case above shows nothing", err)
	}
	got, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil || string(got) != "f\n" {
		t.Errorf("the walk read %q, %v from the folder it holds", got, err)
	}
}

// failing is a kernel open that answers errno.
func failing(errno unix.Errno) sysOpen {
	return func(int, string, int, uint32) (int, error) { return -1, errno }
}

// Each answer of the kernel is the refusal it means: a link, or a name that leaves the folder,
// matches ErrOutside, and so does a component the kernel found not a folder that the walk finds a
// folder or missing, since it changed in between; a file that is not a folder, a missing file, a
// file there already and a denial are the errors os gives for them, a filter's EPERM among them,
// refused and not walked; only a kernel with no openat2 is not handled, and falls back to the walk;
// and open flags no file is opened with are refused before the kernel is asked.
//
// What it costs when this fails: a refusal of the kernel's is read as a plain failure a program
// retries, or a plain failure as a path that left the folder, or a kernel with no openat2 opens
// nothing at all.
func TestEachAnswerOfTheKernelIsTheRefusalItMeans(t *testing.T) {
	t.Parallel()
	cases := []struct {
		what, name string
		errno      unix.Errno
		want       error
	}{
		{"a link the walk cannot find", "a.txt", unix.ELOOP, ErrOutside},
		{"a name that leaves the folder", "a.txt", unix.EXDEV, ErrOutside},
		{"a component not a folder, a folder to the walk", "sub", unix.ENOTDIR, ErrOutside},
		{"a component not a folder, missing to the walk", "gone", unix.ENOTDIR, ErrOutside},
		{"a file that is not a folder", "a.txt", unix.ENOTDIR, unix.ENOTDIR},
		{"a missing file", "a.txt", unix.ENOENT, fs.ErrNotExist},
		{"a file there already", "a.txt", unix.EEXIST, fs.ErrExist},
		{"a denial", "a.txt", unix.EACCES, fs.ErrPermission},
		{"a filter's EPERM, not walked", "a.txt", unix.EPERM, fs.ErrPermission},
	}
	for _, c := range cases {
		f, handled, err := failing(c.errno).open(plain(t), c.name, os.O_RDONLY, 0, func(string) {})
		if f != nil || !handled || !errors.Is(err, c.want) {
			t.Errorf("%s: %v, %v, want %v", c.what, handled, err, c.want)
		}
		if !errors.Is(c.want, ErrOutside) && errors.Is(err, ErrOutside) {
			t.Errorf("%s: %v matches ErrOutside", c.what, err)
		}
	}
	if len(cases) != 9 {
		t.Fatalf("held %d answers", len(cases))
	}
	if f, handled, err := failing(unix.ENOSYS).open(plain(t), "a.txt", os.O_RDONLY, 0, func(string) {}); f != nil || handled || err != nil {
		t.Errorf("no openat2: %v, %v, %v, want it not handled, for the walk", f, handled, err)
	}
	asked := false
	f, handled, err := sysOpen(func(int, string, int, uint32) (int, error) {
		asked = true
		return -1, unix.EINVAL
	}).open(plain(t), "a.txt", -1, 0, func(string) {})
	if f != nil || !handled || err == nil || asked {
		t.Errorf("negative open flags: %v %v %v, asked %v, want them refused before the kernel is asked", f, handled, err, asked)
	}
}

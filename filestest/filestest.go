// Package filestest writes a file, writes a program a test then starts, copies a fixture tree,
// takes a folder a trust walk accepts, and checks a program's names against every system's rules
// and the one list of hostile names, for a program's tests, naming no mode and no temp path, on
// every system. Each call is the library's own call
// under it, and fails the test, saying the path, when that call fails; none skips.
// It imports the standard library, write, appdirs, confine and trust, and no package of the module imports it but its tests.
package filestest

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/appdirs"
	"github.com/birdtable/go-files/confine"
	"github.com/birdtable/go-files/trust"
	"github.com/birdtable/go-files/write"
)

// app names the account's own temp folder Private makes its folders in: <temp>/filestest-<uid>,
// or <temp>\filestest on Windows.
const app = "filestest"

// WriteFile writes content at path as write.File does with no option: the library's private file,
// 0600 on Unix and the owner-only list on Windows, so a test names no mode. It makes no missing
// folder, and fails the test, saying path, on any error.
func WriteFile(t testing.TB, path string, content []byte) {
	t.Helper()
	if err := write.File(path, content); err != nil {
		t.Fatalf("filestest.WriteFile %s: %v", path, err)
	}
}

// WriteProgram writes content at path as write.File does with write.Runnable: 0700 on Unix and
// the owner-only list on Windows, with the fork lock held from the create to the close, so no
// process another goroutine starts meanwhile holds it open for writing and the test can start it
// at once, with no "text file busy". It makes no missing folder, and fails the test, saying path,
// on any error.
func WriteProgram(t testing.TB, path string, content []byte) {
	t.Helper()
	if err := write.File(path, content, write.Runnable()); err != nil {
		t.Fatalf("filestest.WriteProgram %s: %v", path, err)
	}
}

// Private answers a new folder of this account's alone, which trust.Path judges a trust.PrivateFolder
// where on Unix it refuses t.TempDir's, one its group or others may enter. It is one folder
// confine.MkdirFresh makes in the account's own temp folder for the app filestest,
// appdirs.TempOverflow's in a Linux user namespace that maps this account and not root, and
// appdirs.Temp's where that is refused with trust.ErrOverflow, as outside one and off Linux. In one
// that maps root, / is the overflow account's, and Private fails the test. Its path holds no link and
// no test's name, and two calls never answer one folder. At the test's end it is removed with all it
// holds, and a failed removal fails the test. On Windows t.TempDir's folder may pass the walk or not,
// as TEMP is the account's alone or shared, so a test calls Private on every system.
func Private(t testing.TB) string {
	t.Helper()
	base, err := appdirs.TempOverflow(app)
	if errors.Is(err, trust.ErrOverflow) {
		base, err = appdirs.Temp(app)
	}
	if err != nil {
		t.Fatalf("filestest.Private: the account's own temp folder: %v", err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatalf("filestest.Private: %v", err)
	}
	dir, name, err := confine.MkdirFresh(root, "t")
	if err != nil {
		t.Fatalf("filestest.Private: in %s: %v", base, errors.Join(err, root.Close()))
	}
	path := filepath.Join(base, name)
	t.Cleanup(func() {
		if err := errors.Join(root.RemoveAll(name), root.Close()); err != nil {
			t.Errorf("filestest.Private: removing %s at the test's end: %v", path, err)
		}
	})
	if err := dir.Close(); err != nil {
		t.Fatalf("filestest.Private: %s: %v", path, err)
	}
	return path
}

// CopyTree copies the tree at src into the folder at dst, which is there, as confine.CopyTree does
// root to root: each folder 0700 and each file 0600, or 0700 where the source is runnable, on Unix,
// the owner-only list on Windows, and each link that leads inside src made again to the same name in
// dst. A relative path is taken from the working folder. It fails the test, saying both paths, on
// any error, a link that leads outside src or a name already in dst among them.
func CopyTree(t testing.TB, dst, src string) {
	t.Helper()
	if err := copyTree(dst, src); err != nil {
		t.Fatalf("filestest.CopyTree %s to %s: %v", src, dst, err)
	}
}

// copyTree is CopyTree's copy, answering its error.
func copyTree(dst, src string) (err error) {
	to, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	from, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	d, err := os.OpenRoot(to)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	s, err := os.OpenRoot(from)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, s.Close()) }()
	return confine.CopyTree(d, s)
}

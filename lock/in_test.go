package lock

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// openRoot is an os.Root at dir, closed with the test.
func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

// takeIn is TakeIn of name below root, its note at name.holder, released with the test.
func takeIn(t *testing.T, root *os.Root, name string) *Lock {
	t.Helper()
	l, err := TakeIn(root, name, name+".holder")
	if err != nil {
		t.Fatalf("TakeIn(%s): %v", name, err)
	}
	t.Cleanup(func() { _ = l.Release() })
	return l
}

// TakeIn locks a name below a root and writes, reads and removes its note there, by local name:
// a second TakeIn or Take of the same file is declined with Held and told who once the holder
// notes, HolderIn reads the note, Release removes it, and the next TakeIn wins. The root the
// caller passed may be closed once TakeIn returns, as the Lock keeps its own.
//
// What it costs when this fails: a program that holds its state folder as a root locks by a path
// after all, or two runs change the machine at once.
func TestTakeInLocksAndNotesBelowTheRootByLocalName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	l, err := TakeIn(root, "state/app.lock", "state/app.lock.holder")
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil { // the Lock keeps a root of its own
		t.Fatal(err)
	}
	if err := l.Note(map[string]string{"by": "test"}); err != nil {
		t.Fatal(err)
	}
	again := openRoot(t, dir)
	h, raw, err := HolderIn(again, "state/app.lock.holder")
	if err != nil || h.By != os.Getpid() || h.Since.IsZero() || string(raw) != `{"by":"test"}` {
		t.Fatalf("HolderIn: %#v, %s, %v", h, raw, err)
	}
	var held Held
	if got, err := TakeIn(again, "state/app.lock", "state/app.lock.holder"); got != nil || !errors.As(err, &held) || held.By != os.Getpid() {
		t.Errorf("a second TakeIn: %v, %v, and want Held naming this process", got, err)
	}
	path := filepath.Join(dir, "state", "app.lock")
	if got, err := Take(path, path+".holder"); got != nil || !IsHeld(err) {
		t.Errorf("a Take of the same file by its path: %v, %v, and want Held", got, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := WaitIn(ctx, again, "state/app.lock", ""); got != nil || !IsHeld(err) || !errors.Is(err, context.Canceled) {
		t.Errorf("a WaitIn whose context ended: %v, %v, and want Held and context.Canceled", got, err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := again.Lstat("state/app.lock.holder"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the note after Release: %v, and want it removed", err)
	}
	if _, err := l.at.root.Stat("."); !errors.Is(err, fs.ErrClosed) {
		t.Errorf("the Lock's own root after Release: %v, and want it closed", err)
	}
	next, err := WaitIn(t.Context(), again, "state/app.lock", "state/app.lock.holder")
	if err != nil {
		t.Fatalf("WaitIn after the Release: %v", err)
	}
	if err := next.Release(); err != nil {
		t.Error(err)
	}
}

// A lock name or a note that leaves the root, by "..", by a path from the top, or by a link to a
// folder outside it, is refused as os.Root refuses it: TakeIn makes, locks and removes nothing
// outside, and Note writes nothing there. A lock name or a note whose last element is "..", as
// "..", "./.." and "a/../..", is refused by TakeIn and HolderIn before any open.
//
// What it costs when this fails: whoever can put a link in the state folder has a program that
// holds it as a root make, lock or write a file of their choosing outside it, and a note named
// with a last ".." is opened in the folder above the root.
func TestANameOrANoteThatLeavesTheRootIsRefusedAndNothingIsMadeOutside(t *testing.T) {
	t.Parallel()
	top := t.TempDir()
	dir := filepath.Join(top, "state")
	outside := filepath.Join(top, "outside")
	for _, d := range []string{dir, outside} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	plantLink(t, outside, filepath.Join(dir, "out"))
	root := openRoot(t, dir)
	for _, name := range []string{"../outside/app.lock", filepath.Join(outside, "app.lock"), "out/app.lock", "..", "./..", "a/../.."} {
		if l, err := TakeIn(root, name, ""); l != nil || err == nil || IsHeld(err) {
			t.Errorf("TakeIn(%s): %v, %v, and want a refusal", name, l, err)
		}
	}
	keep := filepath.Join(outside, "app.lock.holder")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, note := range []string{"..", "./..", "a/../.."} {
		if l, err := TakeIn(root, "app.lock", note); l != nil || err == nil || IsHeld(err) {
			t.Errorf("TakeIn with the note %s: %v, %v, and want a refusal", note, l, err)
			if l != nil { // a lock taken in error, let go so the next row is its own
				if err := l.Release(); err != nil {
					t.Error(err)
				}
			}
		}
		if h, raw, err := HolderIn(root, note); err == nil || h != (Held{}) || raw != nil {
			t.Errorf("HolderIn(%s): %#v, %s, %v, and want a refusal", note, h, raw, err)
		}
	}
	for _, note := range []string{"../outside/app.lock.holder", "out/app.lock.holder"} {
		l, err := TakeIn(root, "app.lock", note)
		if err != nil {
			t.Fatalf("TakeIn with the note %s: %v", note, err)
		}
		if err := l.Note("mine"); err == nil {
			t.Errorf("Note at %s: nil, and want a refusal", note)
		}
		if h, raw, err := HolderIn(root, note); err == nil || h != (Held{}) || raw != nil {
			t.Errorf("HolderIn(%s): %#v, %s, %v, and want a refusal", note, h, raw, err)
		}
		if err := l.Release(); err == nil {
			t.Errorf("Release with the note %s: nil, and want the root's refusal of its removal", note)
		}
	}
	names, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0].Name() != "app.lock.holder" {
		t.Errorf("outside the root: %v, and want the planted note alone", names)
	}
	if got, err := os.ReadFile(keep); err != nil || string(got) != "keep" {
		t.Errorf("the file outside holds %q, %v", got, err)
	}
}

// A root opened at a folder that is then renamed away, another folder put at its name, still
// takes the lock and writes the note in the folder it was opened at: nothing goes by the path.
//
// On Windows a folder held open as a root cannot be renamed, so the swap does not happen there:
// the rename's sharing refusal is the answer, and the lock and the note are looked for in the
// folder at its own name.
//
// What it costs when this fails: whoever can rename a folder above the state folder has the lock
// taken, and the note written, in a folder of their choosing.
func TestAFolderSwappedAboveTheRootIsNeverReached(t *testing.T) {
	t.Parallel()
	top := t.TempDir()
	dir := filepath.Join(top, "state")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)
	opened, put := filepath.Join(top, "moved"), dir
	if err := os.Rename(dir, opened); inUse(err) { // Windows: the held folder keeps its name
		opened, put = dir, ""
	} else if err != nil {
		t.Fatal(err)
	} else if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	l := takeIn(t, root, "app.lock")
	if err := l.Note("mine"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"app.lock", "app.lock.holder"} {
		if _, err := os.Lstat(filepath.Join(opened, name)); err != nil {
			t.Errorf("%s in the folder the root was opened at: %v", name, err)
		}
		if put == "" {
			continue
		}
		if _, err := os.Lstat(filepath.Join(put, name)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s in the folder put at the name: %v, and want nothing there", name, err)
		}
	}
	if h, _, err := HolderIn(root, "app.lock.holder"); err != nil || h.By != os.Getpid() {
		t.Errorf("HolderIn: %#v, %v", h, err)
	}
}

// A link at the lock's name below the root is refused, its target in the root never made,
// written or locked; one at the note is replaced by Note and removed as the link by TakeIn, its
// target untouched, and HolderIn reads it as a torn note.
//
// The lock taken on the target to show the refusal locked nothing is released before the target
// is read: on Windows a held lock's region refuses another handle's read.
//
// What it costs when this fails: whoever can make a name in the state folder has the lock taken
// on, or the note written over, a file of their choosing.
func TestALinkBelowTheRootIsNeverFollowedAtTheLockOrTheNote(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := openRoot(t, dir)
	keep := []byte("{\"pid\":1,\"since\":\"2026-01-01T00:00:00Z\",\"note\":\"secret\"}\n")
	target := filepath.Join(dir, "precious")
	if err := os.WriteFile(target, keep, 0o600); err != nil {
		t.Fatal(err)
	}
	plantLink(t, "precious", filepath.Join(dir, "app.lock"))
	if l, err := TakeIn(root, "app.lock", ""); l != nil || err == nil || IsHeld(err) || !errors.Is(err, errLink) {
		t.Errorf("TakeIn through a link: %v, %v, and want errLink", l, err)
	}
	if err := takeIn(t, root, "precious").Release(); err != nil { // the refusal locked nothing
		t.Fatal(err)
	}
	plantLink(t, "missing", filepath.Join(dir, "dangling.lock"))
	if l, err := TakeIn(root, "dangling.lock", ""); l != nil || err == nil {
		t.Errorf("TakeIn through a dangling link: %v, %v", l, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("TakeIn through a dangling link made its target: %v", err)
	}

	plantLink(t, "precious", filepath.Join(dir, "other.lock.holder"))
	l := takeIn(t, root, "other.lock")
	if _, err := os.Lstat(filepath.Join(dir, "other.lock.holder")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the link left at the note: %v, and want it removed by TakeIn", err)
	}
	plantLink(t, "precious", filepath.Join(dir, "other.lock.holder"))
	if h, raw, err := HolderIn(root, "other.lock.holder"); err != nil || h != (Held{}) || raw != nil {
		t.Errorf("HolderIn through a link: %#v, %s, %v, and want Held{} read from no file", h, raw, err)
	}
	if err := l.Note("mine"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(filepath.Join(dir, "other.lock.holder")); err != nil || !fi.Mode().IsRegular() {
		t.Errorf("the note is %v, %v, and want a plain file", fi.Mode(), err)
	}
	if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, keep) {
		t.Errorf("the target holds %q, %v", got, err)
	}
}

// A note whose local name cleans to the lock's is refused before anything is opened, and a
// missing or torn note below the root reads as Held{}, never as free and never as an error.
//
// What it costs when this fails: Note writes over the lock file's name, or a reader takes a note
// it cannot read for a free lock.
func TestTakeInRefusesTheLocksOwnNameAsTheNoteAndReadsATornNoteAsHeld(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := openRoot(t, dir)
	if l, err := TakeIn(root, "app.lock", "./app.lock"); l != nil || !errors.Is(err, errNoteIsLock) {
		t.Errorf("TakeIn with the lock's own name as the note: %v, %v", l, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "app.lock")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the refused TakeIn made the lock file: %v", err)
	}
	if h, raw, err := HolderIn(root, "none.holder"); err != nil || h != (Held{}) || raw != nil {
		t.Errorf("HolderIn of no note: %#v, %s, %v", h, raw, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "torn.holder"), []byte(`{"pid":7`), 0o600); err != nil {
		t.Fatal(err)
	}
	if h, raw, err := HolderIn(root, "torn.holder"); err != nil || h != (Held{}) || raw != nil {
		t.Errorf("HolderIn of a torn note: %#v, %s, %v", h, raw, err)
	}
}

// Below a root, whether the note is beside the lock file, the one folder where Note gives it the
// lock file's owner, is read through the root: true for the lock file's own folder, false for
// another, whatever the working folder holds.
//
// What it costs when this fails: a note in another folder is handed the lock file's owner, or
// one beside it is not, as the folders were looked up by a path from the working folder.
func TestBesideBelowARootComparesTheFoldersThroughIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, d := range []string{"a", "b"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	at := in{openRoot(t, dir)}
	if !beside(at, "a/app.lock", "a/app.lock.holder") {
		t.Error("a note in the lock file's own folder below the root is not beside it")
	}
	if beside(at, "a/app.lock", "b/app.lock.holder") {
		t.Error("a note in another folder below the root is beside the lock file")
	}
}

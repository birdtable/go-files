package lock

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// A symbolic link at the lock path is refused, dangling or not, and the file it leads to is not
// made, written, changed or locked.
//
// What it costs when this fails: whoever can put a link where a program keeps its lock has it
// lock, or make, a file of their choosing.
func TestALinkAtTheLockPathIsRefusedAndItsTargetUntouched(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "precious")
	if err := os.WriteFile(target, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "app.lock")
	plantLink(t, target, path)
	was, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if l, err := Take(path, path+".holder"); l != nil || err == nil || IsHeld(err) {
		t.Fatalf("Take through a link: %v, %v, and want a refusal", l, err)
	}
	if fi, err := os.Lstat(target); err != nil || fi.Mode() != was.Mode() {
		t.Errorf("the target is %v, %v", fi.Mode(), err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "keep\n" {
		t.Errorf("the target holds %q, %v", got, err)
	}
	take(t, target)

	missing := filepath.Join(dir, "missing")
	dangling := filepath.Join(dir, "dangling.lock")
	plantLink(t, missing, dangling)
	if l, err := Take(dangling, dangling+".holder"); l != nil || err == nil {
		t.Errorf("Take through a dangling link: %v, %v", l, err)
	}
	if _, err := os.Lstat(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Take through a dangling link made its target: %v", err)
	}
}

// A link at <name>.holder is replaced by Note, never followed, and its target is untouched;
// Holder reads such a link as a torn note, without following it.
//
// What it costs when this fails: whoever can write the lock's folder has a program that holds a
// lock write over a file of their choosing, or read one back as the note.
func TestALinkAtTheNoteIsReplacedAndItsTargetUntouched(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "precious")
	keep := []byte("{\"pid\":1,\"since\":\"2026-01-01T00:00:00Z\",\"note\":\"secret\"}\n")
	if err := os.WriteFile(target, keep, 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "app.lock")
	l := take(t, path)
	plantLink(t, target, path+".holder")
	if h, note, err := Holder(path + ".holder"); err != nil || h != (Held{}) || note != nil {
		t.Errorf("Holder through a link: %#v, %s, %v, and want Held{} read from no file", h, note, err)
	}
	if err := l.Note("mine"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(path + ".holder"); err != nil || !fi.Mode().IsRegular() {
		t.Errorf("the note is %v, %v, and want a plain file", fi.Mode(), err)
	}
	if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, keep) {
		t.Errorf("the target holds %q, %v", got, err)
	}
	if h, note, err := Holder(path + ".holder"); err != nil || h.By != os.Getpid() || string(note) != `"mine"` {
		t.Errorf("Holder: %#v, %s, %v", h, note, err)
	}
}

// A link left at <name>.holder before Take, where no holder wrote it, is removed by Take as the
// link, never followed: its target is untouched, and the lock is taken.
//
// What it costs when this fails: whoever can write the lock's folder plants a link there, and the
// next program to take the lock removes or writes the file it leads to.
func TestTakeRemovesALinkLeftAtTheNoteAndNotItsTarget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "precious")
	if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "app.lock")
	plantLink(t, target, path+".holder")
	l := take(t, path)
	if _, err := os.Lstat(path + ".holder"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the link left at the note: %v, and want it removed by Take", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "keep" {
		t.Errorf("the link's target holds %q, %v, and want \"keep\"", got, err)
	}
	if err := l.Release(); err != nil {
		t.Error(err)
	}
}

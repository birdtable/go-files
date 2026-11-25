package write

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// On a volume that keeps no access lists, File, Keep and Append's new log are refused with
// ErrNoAccessList, which matches errors.ErrUnsupported, and nothing is made or written; an Append
// to a log there already writes, as it makes nothing.
//
// What it costs when this fails: a key, its kept copy or an audit log written to a FAT or exFAT
// volume is every account's to read, while the program believes it private.
func TestAFileMadeOnAVolumeWithNoAccessListsIsRefusedAndNothingIsWritten(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	refused := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, ErrNoAccessList) || !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("%s on a volume with no access lists: %v, and want ErrNoAccessList matching errors.ErrUnsupported", what, err)
		}
	}
	refused("File", File(filepath.Join(dir, "key"), []byte("secret"), NoSync(), listless()))
	alone(t, dir)
	put0(t, filepath.Join(dir, "app.toml"), "old")
	refused("File over a file there", File(filepath.Join(dir, "app.toml"), []byte("new"), NoSync(), listless()))
	holds(t, filepath.Join(dir, "app.toml"), "old")
	_, err := Keep(filepath.Join(dir, "app.toml"), dir, "app.toml.kept", NoSync(), listless())
	refused("Keep", err)
	alone(t, dir, "app.toml")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	refused("Append to a new log", Append(t.Context(), root, "audit.log", []byte("line"), 100, Aside("audit.old"), LogOption(listless())))
	alone(t, dir, "app.toml")
	put0(t, filepath.Join(dir, "audit.log"), "old\n")
	if err := Append(t.Context(), root, "audit.log", []byte("line"), 100, Aside("audit.old"), LogOption(listless())); err != nil {
		t.Fatalf("Append to a log there, on a volume with no access lists: %v", err)
	}
	holds(t, filepath.Join(dir, "audit.log"), "old\nline\n")
}

// listless takes every volume as one that keeps no access lists, which no test machine's NTFS is.
func listless() Option {
	return func(o *options) { o.listless = true }
}

// Append to a log whose name ends in a dot is refused at its door, matching fs.ErrInvalid, before
// anything is opened: with no log there nothing is made, and with c.log there it is left as it
// was. os.Root's look reads c.log. as c.log, and the make keeps the dot.
//
// What it costs when this fails: Append makes a log its own look never matches, or holds c.log
// while it looks for c.log., tries again until its tries are spent, and ends in an untyped error,
// leaving a file nothing can open by its name.
func TestAppendToALogEndingInADotIsRefusedAtTheDoorAndChangesNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	if err := Append(t.Context(), root, "c.log.", []byte("line"), 100, Aside("c.old")); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("Append to c.log. with no log there: %v, and want a refusal matching fs.ErrInvalid", err)
	}
	if left, err := os.ReadDir(dir); err != nil || len(left) != 0 {
		t.Errorf("a refused Append left %v (%v), and want nothing", left, err)
	}
	put0(t, filepath.Join(dir, "c.log"), "old\n")
	if err := Append(t.Context(), root, "c.log.", []byte("line"), 100, Aside("c.old")); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("Append to c.log. with c.log there: %v, and want a refusal matching fs.ErrInvalid", err)
	}
	holds(t, filepath.Join(dir, "c.log"), "old\n")
	alone(t, dir, "c.log")
}

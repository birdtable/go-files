package write

import (
	"crypto/sha256"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
)

// Shared with Mode, in either order, is refused with fs.ErrInvalid by File, Keep and PutBack before
// anything is made or replaced, on every system.
//
// What it costs when this fails: a caller that names both gets one of the two silently, a file
// readable to every account where it asked 0600, or the other way round.
func TestSharedWithModeIsRefusedBeforeAnythingIsMade(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	put0(t, filepath.Join(dir, "app.toml"), "old")
	for what, opts := range map[string][]Option{
		"Shared then Mode": {Shared(), Mode(0o600)},
		"Mode then Shared": {Mode(0o644), Shared()},
	} {
		if err := File(filepath.Join(dir, "new.toml"), []byte("new"), opts...); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("File, %s: %v, and want fs.ErrInvalid", what, err)
		}
		if err := File(filepath.Join(dir, "app.toml"), []byte("new"), opts...); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("File over a file, %s: %v, and want fs.ErrInvalid", what, err)
		}
		if _, err := Keep(filepath.Join(dir, "app.toml"), dir, "app.toml.kept", opts...); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Keep, %s: %v, and want fs.ErrInvalid", what, err)
		}
		alone(t, dir, "app.toml")
		holds(t, filepath.Join(dir, "app.toml"), "old")
	}
	put0(t, filepath.Join(dir, "app.toml.kept"), "kept")
	for what, opts := range map[string][]Option{
		"Shared then Mode": {Shared(), Mode(0o600)},
		"Mode then Shared": {Mode(0o644), Shared()},
	} {
		err := PutBack(filepath.Join(dir, "app.toml.kept"), filepath.Join(dir, "app.toml"), sha256.Sum256([]byte("old")), opts...)
		if !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("PutBack, %s: %v, and want fs.ErrInvalid", what, err)
		}
		alone(t, dir, "app.toml", "app.toml.kept")
		holds(t, filepath.Join(dir, "app.toml"), "old")
	}
}

// Keep and PutBack take Shared and do not apply it: the copy has its source's mode, and the file
// put back the copy's (on Windows the owner-only list); on Unix KeepMode with Shared keeps an old
// file's mode, and is 0644 where there is none; KeepOwner with Shared is 0644.
//
// What it costs when this fails: a copy of a secret made with Shared to restore it later is
// readable to every account, or a secret rewritten with KeepMode and Shared is widened to 0644.
func TestSharedUnderKeepModeKeepOwnerKeepAndPutBack(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	key := filepath.Join(dir, "key")
	put0(t, key, "secret") // 0600
	kept, err := Keep(key, dir, "key.kept", Shared(), NoSync())
	if err != nil {
		t.Fatal(err)
	}
	permIs(t, kept, 0o600)
	if err := PutBack(kept, key, sha256.Sum256([]byte("secret")), Shared(), NoSync()); err != nil {
		t.Fatal(err)
	}
	permIs(t, key, 0o600)
	if err := File(key, []byte("again"), KeepMode(), Shared(), NoSync()); err != nil {
		t.Fatal(err)
	}
	permIs(t, key, 0o600)
	if err := File(filepath.Join(dir, "fresh"), []byte("x"), KeepMode(), Shared(), NoSync()); err != nil {
		t.Fatal(err)
	}
	permIs(t, filepath.Join(dir, "fresh"), 0o644)
	if err := File(filepath.Join(dir, "fresh"), []byte("y"), KeepOwner(), Shared(), NoSync()); err != nil {
		t.Fatal(err)
	}
	permIs(t, filepath.Join(dir, "fresh"), 0o644)
}

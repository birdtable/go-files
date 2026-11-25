package confine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// On macOS renameatx_np takes both flags, so its EINVAL is the caller's own: a folder moved into
// itself, or swapped with its own child. Neither rename reads it as errors.ErrUnsupported or
// ErrNoExchange, and nothing moves.
//
// What it costs when this fails: a caller told that the system cannot do the move takes another
// path, such as a lock and a plain rename, for a call that is refused for its own names.
func TestOnMacOSAFolderRenamedIntoItselfIsNotUnsupported(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	if err := os.Mkdir(filepath.Join(dir, "real", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	calls := []struct {
		what string
		err  error
	}{
		{"RenameNoReplace(real, real/moved)", confine.RenameNoReplace(root, "real", filepath.Join("real", "moved"))},
		{"RenameExchange(real, real/sub)", confine.RenameExchange(root, "real", filepath.Join("real", "sub"))},
	}
	for _, c := range calls {
		if c.err == nil || errors.Is(c.err, errors.ErrUnsupported) || errors.Is(c.err, confine.ErrNoExchange) {
			t.Errorf("%s = %v, want a refusal that is neither errors.ErrUnsupported nor ErrNoExchange", c.what, c.err)
		}
	}
	if info, err := os.Lstat(filepath.Join(dir, "real", "sub")); err != nil || !info.IsDir() {
		t.Errorf("real/sub is %v, %v, want the folder where it was", info, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "real", "moved")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("real/moved is there: %v", err)
	}
}

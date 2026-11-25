//go:build linux || darwin

package confine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// swaps is whether the system swaps two names in one step: renameat2 with RENAME_EXCHANGE on
// Linux, renameatx_np with RENAME_SWAP on macOS.
const swaps = true

// RenameExchange swaps two files, and two folders, each in one call, and refuses a missing name
// with fs.ErrNotExist, leaving the other as it was.
//
// What it costs when this fails: an exchange leaves the two names half swapped, or a swap with a
// name that is gone moves the other in its place.
func TestRenameExchangeSwapsTwoFilesAndTwoFolders(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	if err := confine.RenameExchange(root, filepath.Join("real", "HEAD"), "beside.txt"); err != nil {
		t.Fatalf("RenameExchange of two files: %v", err)
	}
	if err := confine.RenameExchange(root, "real", "deep"); err != nil {
		t.Fatalf("RenameExchange of two folders: %v", err)
	}
	if err := confine.RenameExchange(root, "beside.txt", "gone"); !errors.Is(err, fs.ErrNotExist) || errors.Is(err, confine.ErrOutside) {
		t.Errorf("RenameExchange with a missing name: %v, want fs.ErrNotExist", err)
	}
	want := map[string]string{
		"deep/HEAD":   "beside\n",
		"beside.txt":  "head\n",
		"deep/config": "config\n",
		"real/a.txt":  "a\n",
	}
	for p, body := range want {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil || string(got) != body {
			t.Errorf("%s holds %q, %v, want %q", p, got, err, body)
		}
	}
	if len(want) != 4 {
		t.Fatalf("checked %d files", len(want))
	}
	if _, err := os.Lstat(filepath.Join(dir, "gone")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("gone is there: %v", err)
	}
}

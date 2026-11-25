//go:build !linux && !darwin && !windows

package confine_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// On FreeBSD and the other systems with no rename that refuses to replace a name and follows no
// link, RenameNoReplace refuses with errors.ErrUnsupported, which is not ErrOutside, and moves
// nothing; RenameExchange refuses with ErrNoExchange, which matches errors.ErrUnsupported.
//
// What it costs when this fails: a rename on FreeBSD replaces a file another process made, as a
// look before a plain rename can, or the refusal reads as a path that left the folder.
func TestOnFreeBSDBothRenamesRefuseAndMoveNothing(t *testing.T) {
	t.Parallel()
	root, dir := plainFolder(t)
	err := confine.RenameNoReplace(root, "beside.txt", "moved")
	if !errors.Is(err, errors.ErrUnsupported) || errors.Is(err, confine.ErrOutside) {
		t.Errorf("RenameNoReplace: %v, want errors.ErrUnsupported and not ErrOutside", err)
	}
	err = confine.RenameExchange(root, "beside.txt", filepath.Join("real", "HEAD"))
	if !errors.Is(err, confine.ErrNoExchange) || !errors.Is(err, errors.ErrUnsupported) || errors.Is(err, confine.ErrOutside) {
		t.Errorf("RenameExchange: %v, want ErrNoExchange and errors.ErrUnsupported", err)
	}
	unchanged(t, dir)
	if _, err := os.Lstat(filepath.Join(dir, "moved")); err == nil {
		t.Error("moved is there")
	}
}

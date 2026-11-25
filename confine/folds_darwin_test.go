package confine_test

import (
	"errors"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// foldsFolders is the folders beyond the temp folder that Folds is held to on macOS: devfs, which
// it cannot ask. A case-sensitive APFS volume needs a disk image attached, which a runner may not
// allow, so its answer is held by the rule's table.
func foldsFolders(*testing.T) []string { return []string{"/dev"} }

// On macOS a folder on devfs, a volume neither APFS nor HFS+, is FoldUnknown with ErrNoFolding.
//
// What it costs when this fails: a volume Folds cannot read is answered as one that tells names
// apart, and a caller writes two names that are one file.
func TestFoldsAnswersUnknownOnDevfs(t *testing.T) {
	t.Parallel()
	if got, err := confine.Folds("/dev"); got != confine.FoldUnknown || !errors.Is(err, confine.ErrNoFolding) {
		t.Errorf("Folds(/dev) = %v, %v, want FoldUnknown and ErrNoFolding", got, err)
	}
}

package confine_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
	"golang.org/x/sys/unix"
)

// foldsFolders is the folders beyond the temp folder that Folds is held to on Linux: /proc, which
// it cannot ask; /dev/shm, a tmpfs folder; and an empty folder given casefold, where the file
// system and the account let it be set.
func foldsFolders(t *testing.T) []string {
	t.Helper()
	dirs := []string{"/proc"}
	if info, err := os.Stat("/dev/shm"); err == nil && info.IsDir() {
		dirs = append(dirs, "/dev/shm")
	}
	if dir, err := casefolded(t); err != nil {
		t.Logf("no casefold folder on this runner: %v", err)
	} else {
		dirs = append(dirs, dir)
	}
	return dirs
}

// casefolded is an empty folder in the temp folder with casefold set, or why it cannot be set.
func casefolded(t *testing.T) (string, error) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "casefold")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	flags, err := unix.IoctlGetUint32(int(f.Fd()), unix.FS_IOC_GETFLAGS)
	if err == nil {
		err = unix.IoctlSetPointerInt(int(f.Fd()), unix.FS_IOC_SETFLAGS, int(flags|confine.FSCasefoldFL))
	}
	return dir, err
}

// On Linux a folder on procfs, a file system Folds does not ask, is FoldUnknown with ErrNoFolding.
//
// What it costs when this fails: a file system Folds cannot read is answered as one that tells
// names apart, and a caller writes two names that are one file.
func TestFoldsAnswersUnknownOnProc(t *testing.T) {
	t.Parallel()
	if got, err := confine.Folds("/proc"); got != confine.FoldUnknown || !errors.Is(err, confine.ErrNoFolding) {
		t.Errorf("Folds(/proc) = %v, %v, want FoldUnknown and ErrNoFolding", got, err)
	}
}

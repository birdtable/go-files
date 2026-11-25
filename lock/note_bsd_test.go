//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package lock

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A note in another folder than the lock file's is written whatever the lock file's group, and
// is this process's, with the lock file's mode. On macOS and the BSDs a new file
// takes its folder's group, so a lock file under /tmp is in wheel's, which this account is not
// in, and the note's temp file in another folder is not: a chown to the lock file's owner and
// group is refused there, so the note is not given them.
//
// What it costs when this fails: every Note fails where the lock and its note are in different
// folders, and no declined run is ever told who holds the lock; or, run as root, a note is
// handed to another account in a folder that account could not write.
func TestANoteInAnotherFolderIsThisProcesssWhateverTheLockFilesGroup(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Fatal("run as root, which may give a file any group: the test tier runs as an account that is not root")
	}
	tmp, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(tmp, "go-files-lock-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	path := filepath.Join(dir, "app.lock")
	note := filepath.Join(t.TempDir(), "who.json")
	l, err := Take(path, note)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Release() })
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	_, gid := ownerOf(t, path)
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(groups, int(gid)) || int(gid) == os.Getegid() {
		t.Fatalf("the lock file's group %d, its folder's under %s, is this account's, and the row needs one it is not in", gid, tmp)
	}

	if err := l.Note("mine"); err != nil {
		t.Fatalf("Note in another folder than the lock file's, whose group %d this account is not in: %v", gid, err)
	}
	if h, raw, err := Holder(note); err != nil || h.By != os.Getpid() || string(raw) != `"mine"` {
		t.Errorf("Holder: %#v, %s, %v, and want this process and \"mine\"", h, raw, err)
	}
	if uid, _ := ownerOf(t, note); uid != uint32(os.Geteuid()) {
		t.Errorf("the note's owner is %d, and want this process's %d", uid, os.Geteuid())
	}
	if fi, err := os.Lstat(note); err != nil || fi.Mode() != 0o640 {
		t.Errorf("the note is %v, %v, and want the lock file's -rw-r-----", fi.Mode(), err)
	}
}

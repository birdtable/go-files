//go:build unix

package trust

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// judgedDir is a folder of the test's own that Path passes as a Folder, with no link in it.
func judgedDir(t *testing.T) string { t.Helper(); return realDir(t) }

// plantFolderLink makes at a link to the folder target, which need not be there.
func plantFolderLink(t *testing.T, at, target string) {
	t.Helper()
	if err := os.Symlink(target, at); err != nil {
		t.Fatal(err)
	}
}

// openToOthers lets every account read and search the folder p.
func openToOthers(t *testing.T, p string) {
	t.Helper()
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

// writableByOthers lets every account change the folder p, with no sticky bit.
func writableByOthers(t *testing.T, p string) {
	t.Helper()
	if err := os.Chmod(p, 0o777); err != nil {
		t.Fatal(err)
	}
}

// What it costs when this fails: a folder made and not there to a second look, as Windows answers
// for a moment while another make is under way, stops the program where the next try would make it.
// Unix alone: Windows will not remove a folder this call holds open.
func TestADirTakenAwayAfterTheMakeIsMadeAgain(t *testing.T) {
	t.Parallel()
	base := judgedDir(t)
	dir := filepath.Join(base, "a", "b")
	gone := false
	r, err := mkdirPrivate(dir, Who{NoLinks: true}, func(step string) {
		if step != stepMade || gone {
			return
		}
		gone = true
		if err := os.Remove(dir); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatalf("MkdirPrivate with dir taken away after the make: %v", err)
	}
	defer r.Close()
	if !gone {
		t.Fatal("MkdirPrivate never reached the step after the make")
	}
	sameAs(t, r, dir)
}

// What it costs when this fails: another folder renamed in at dir after the make passes the judge
// of dir, and the call answers the folder it made, which is no longer at dir, so the program keeps
// its state where no name leads, or the judge's verdict is of a folder the program never uses.
func TestAFolderSwappedInAtDirAfterTheMakeIsRefusedAndTheMadeOneNotAnswered(t *testing.T) {
	t.Parallel()
	base := judgedDir(t)
	dir := filepath.Join(base, "a", "b")
	swapped := false
	r, err := mkdirPrivate(dir, Who{NoLinks: true}, func(step string) {
		if step != stepMade || swapped {
			return
		}
		swapped = true
		if err := os.Rename(dir, dir+".made"); err != nil {
			t.Error(err)
		}
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Error(err)
		}
	})
	if err == nil {
		r.Close()
	}
	if !swapped {
		t.Fatal("MkdirPrivate never reached the step after the make")
	}
	wantRefused(t, "MkdirPrivate with another folder renamed in at dir after the make", err,
		refusal{want: ErrReplaced, says: "another folder than the one made or opened"})
}

// What it costs when this fails: a walk of the folders above that a folder this account cannot
// search cut short, as it was made so after the look, sends the call on to a make below it, where
// the walk's refusal is the answer. Unix alone, as a mode; judgedDir refuses to run as root, which
// searches any folder.
func TestAWalkCutShortByAnythingButANameNotThereIsAnsweredAsItRefused(t *testing.T) {
	t.Parallel()
	base := judgedDir(t)
	top := filepath.Join(base, "top")
	if err := os.Mkdir(top, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(top, 0o700) })
	locked := false
	r, err := mkdirPrivate(filepath.Join(top, "a"), Who{NoLinks: true}, func(step string) {
		if step != stepFound || locked {
			return
		}
		locked = true
		if err := os.Chmod(top, 0o000); err != nil {
			t.Error(err)
		}
	})
	if err == nil {
		r.Close()
	}
	if !locked {
		t.Fatal("MkdirPrivate never reached the step before the walk")
	}
	pe, ok := errors.AsType[*fs.PathError](err)
	if !ok || !errors.Is(err, fs.ErrPermission) || pe.Op != "lstat" || strings.Contains(err.Error(), "making below") {
		t.Errorf("MkdirPrivate with top made unsearchable before the walk = %v, and want the walk's lstat refusal, with no make tried", err)
	}
}

// What it costs when this fails: a link at the nearest part retargeted after the walk of the
// folders above leads the make into the folder the walk reached, which dir no longer names, so a
// folder is made where the caller never asked. Unix alone: os.Symlink to a folder.
func TestALinkAtTheNearestPartRetargetedAfterTheWalkMakesNothingWhereItLedBefore(t *testing.T) {
	t.Parallel()
	base := judgedDir(t)
	was, now, link := filepath.Join(base, "was"), filepath.Join(base, "now"), filepath.Join(base, "link")
	for _, p := range []string{was, now} {
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	plantFolderLink(t, link, was)
	moved := false
	r, err := mkdirPrivate(filepath.Join(link, "a"), Who{}, func(step string) {
		if step != stepWalked || moved {
			return
		}
		moved = true
		if err := os.Remove(link); err != nil {
			t.Error(err)
		}
		plantFolderLink(t, link, now)
	})
	if err == nil {
		r.Close()
	}
	if !moved {
		t.Fatal("MkdirPrivate never reached the step after the walk")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("MkdirPrivate through a link retargeted after the walk = %v, and want the walk's refusal of the name not there", err)
	}
	missing(t, filepath.Join(was, "a"))
}

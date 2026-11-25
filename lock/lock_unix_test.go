//go:build unix

package lock

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"weak"
)

// ownerOf is a file's uid and gid.
func ownerOf(t *testing.T, path string) (uint32, uint32) {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("%s's stat says no owner", path)
	}
	return st.Uid, st.Gid
}

// A lock file Take makes is 0600 under any umask, a narrower one included, so the mode is set on
// the descriptor, not left to the umask.
//
// What it costs when this fails: another account reads or takes a program's lock, or the
// program's own next run may not open it.
func TestALockFileTakeMakesIs0600UnderAnyUmask(t *testing.T) {
	t.Parallel()
	// Each umask is a child process, and under -race each child sleeps a second at its exit, so the
	// umasks run at once. A cleanup runs once every umask has ended, and counts them.
	var tried atomic.Int64
	t.Cleanup(func() {
		if tried := tried.Load(); tried != 3 {
			t.Errorf("tried %d umasks", tried)
		}
	})
	for _, mask := range []string{"0", "077", "0277"} {
		t.Run(mask, func(t *testing.T) {
			t.Parallel()
			path := tempLock(t)
			cmd := helperCmd(t, "umask", mask, path)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("umask %s: %v, %s", mask, err, out)
			}
			fi, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode() != 0o600 {
				t.Errorf("umask %s: the lock file is %v, and want -rw-------", mask, fi.Mode())
			}
			tried.Add(1)
		})
	}
}

// A lock file that is there keeps its mode and owner, and .holder takes both. The group is one of
// this account's that is not the folder's when it has one: a new file takes its folder's group on
// macOS, so only then is a chown that did not happen seen.
//
// What it costs when this fails: a program that shares its lock with a group finds it narrowed
// to its own account, or the note is readable by accounts the lock file is not.
func TestALockFileThereKeepsItsModeAndOwnerAndTheNoteTakesThem(t *testing.T) {
	t.Parallel()
	path := tempLock(t)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, folder := ownerOf(t, filepath.Dir(path))
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range append(groups, os.Getgid()) {
		if uint32(g) != folder && os.Chown(path, -1, g) == nil {
			break
		}
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	uid, gid := ownerOf(t, path)
	t.Logf("the lock file's group is %d and its folder's %d", gid, folder)
	if err := take(t, path).Note(nil); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(path); err != nil || fi.Mode() != 0o640 {
		t.Errorf("the lock file is %v, %v, and want -rw-r-----", fi.Mode(), err)
	}
	if u, g := ownerOf(t, path); u != uid || g != gid {
		t.Errorf("the lock file is %d:%d, and was %d:%d", u, g, uid, gid)
	}
	if fi, err := os.Lstat(path + ".holder"); err != nil || fi.Mode() != 0o640 {
		t.Errorf("the note is %v, %v, and want -rw-r-----", fi.Mode(), err)
	}
	if u, g := ownerOf(t, path+".holder"); u != uid || g != gid {
		t.Errorf("the note is %d:%d, and want the lock file's %d:%d", u, g, uid, gid)
	}
}

// A lock file this account may not open to write is refused with fs.ErrPermission, by Take and
// by Wait, at once and never as Held; so is a missing lock file in a folder this account may not
// write, and nothing is made there.
//
// What it costs when this fails: a person who forgot sudo holds the lock root's run needs, or is
// told another run holds it.
func TestALockFileThisAccountMayNotWriteIsRefused(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Fatal("run as root, which opens every file: the test tier runs as an account that is not root")
	}
	path := tempLock(t)
	if err := os.WriteFile(path, nil, 0o400); err != nil {
		t.Fatal(err)
	}
	shut := filepath.Join(t.TempDir(), "shut")
	if err := os.Mkdir(shut, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(shut, 0o700); err != nil {
			t.Error(err)
		}
	})
	missing := filepath.Join(shut, "app.lock")
	for what, path := range map[string]string{"a lock file there": path, "a missing lock file in a folder": missing} {
		for name, try := range map[string]func() (*Lock, error){
			"Take": func() (*Lock, error) { return Take(path, path+".holder") },
			"Wait": func() (*Lock, error) { return Wait(t.Context(), path, path+".holder") },
		} {
			if l, err := try(); l != nil || !errors.Is(err, fs.ErrPermission) || IsHeld(err) || !strings.Contains(err.Error(), path) {
				t.Errorf("%s of %s this account may not write: %v, %v, and want fs.ErrPermission naming the file", name, what, l, err)
			}
		}
	}
	if names, err := os.ReadDir(shut); err != nil || len(names) != 0 {
		t.Errorf("the folder this account may not write holds %v (%v), and want nothing made", names, err)
	}
}

// Release drops the lock while another descriptor shares the open file it locks, as a fork on its
// way to exec does in any process that starts children.
//
// What it costs when this fails: a program that starts any child while it holds the lock may find
// its lock still held after Release, and its own next Take refused.
func TestReleaseDropsTheLockThatAForkStillShares(t *testing.T) {
	t.Parallel()
	path := tempLock(t)
	l := take(t, path)
	fd, err := syscall.Dup(int(l.f.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(fd) })
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	take(t, path)
}

// What is put at the lock's name after Take opened and checked the file there is never locked in
// its place: after the lock, the name is read again, and the lock is refused unless that is still
// the file Take opened. A plain file renamed over the name, a link put there or a pipe put there is
// refused as not the file at its name, the plain file is left unlocked, and the file Take locked is
// let go. On Windows the file Take holds open cannot be renamed over or removed, so the swap does
// not happen there.
//
// What it costs when this fails: a pipe put at the name after the check is held as the lock, past
// the refusal of what is not a plain file, and the note takes the mode and owner of a file no
// longer there.
func TestWhatIsPutAtTheNameAfterTheCheckIsNeverLocked(t *testing.T) {
	t.Parallel()
	for name, put := range map[string]func(t *testing.T, path string){
		"a plain file": func(t *testing.T, path string) {
			t.Helper()
			if err := os.Link(path, path+".first"); err != nil { // the file Take locked keeps a name
				t.Fatal(err)
			}
			other := path + ".other"
			if err := os.WriteFile(other, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(other, path); err != nil {
				t.Fatal(err)
			}
		},
		"a link": func(t *testing.T, path string) {
			t.Helper()
			other := path + ".other"
			if err := os.WriteFile(other, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			plantLink(t, other, path)
		},
		"a pipe": func(t *testing.T, path string) {
			t.Helper()
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := tempLock(t)
			swapped := 0
			l, err := acquire(in{}, path, path+".holder", nil, func() { put(t, path); swapped++ }, false)
			if swapped != 1 {
				t.Fatalf("swapped %d times", swapped)
			}
			if l != nil || err == nil || IsHeld(err) || !strings.Contains(err.Error(), path) {
				t.Fatalf("Take: %v, %v, and want a refusal naming the file", l, err)
			}
			if !errors.Is(err, errLink) {
				t.Errorf("Take: %v, and want errLink", err)
			}
			if name == "a plain file" {
				take(t, path)
				take(t, path+".first") // the refusal unlocked and closed the file it locked
			}
		})
	}
}

// Stat is the file Take locked, not whatever is at the name now: a plain file renamed over the
// name after Take is not the file Stat says.
//
// What it costs when this fails: a caller that checks its lock against the name finds the
// file put there and takes it for the one it holds.
func TestStatIsTheFileTakeLockedNotWhatIsAtTheNameNow(t *testing.T) {
	t.Parallel()
	path := tempLock(t)
	l := take(t, path)
	other := path + ".other"
	if err := os.WriteFile(other, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(other, path); err != nil {
		t.Fatal(err)
	}
	at, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := l.Stat(); err != nil || os.SameFile(got, at) {
		t.Fatalf("Stat: %v, %v, and want the file Take locked, not the one now at its name", got, err)
	}
}

// A Lock the caller keeps no reference to is held until the process exits, as Lock says: a
// collection never closes its file and drops the lock under it. On Windows the same finalizer
// closes the handle; the keeper is the same code there.
//
// What it costs when this fails: a program that takes its lock for its whole run and keeps no
// *Lock loses the lock at the next collection, and a second run takes it while the first runs.
func TestALockNobodyKeepsIsHeldPastEveryCollection(t *testing.T) {
	// serial: collects the process's garbage, which closes a file another test leaked and lets its lock go
	path := tempLock(t)
	forgot := takeAndForget(t, path)
	t.Cleanup(func() { _ = forgot.Value().Release() })
	for i := range 50 {
		runtime.GC()
		runtime.Gosched()
		if l, err := Take(path, path+".holder"); !IsHeld(err) {
			_ = l.Release()
			t.Fatalf("after %d collections a second Take is %v, %v, and want Held", i+1, l, err)
		}
	}
}

// takeAndForget takes path and keeps only a weak pointer to the Lock, which the collector does not
// count as a reference.
//
//go:noinline
func takeAndForget(t *testing.T, path string) weak.Pointer[Lock] {
	t.Helper()
	l, err := Take(path, path+".holder")
	if err != nil {
		t.Fatal(err)
	}
	return weak.Make(l)
}

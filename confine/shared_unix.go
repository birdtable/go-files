//go:build unix

package confine

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// linkCount is the count of names the volume reports for the file of info, its st_nlink.
func linkCount(_ *os.File, info fs.FileInfo) (int, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("this system's stat holds no count of names")
	}
	return countOf(uint64(st.Nlink)), nil // uint16 on macOS, uint32 or uint64 elsewhere
}

// noWait opens a pipe at the name with no wait for its other end, so an open never hangs on one
// swapped in.
const noWait = syscall.O_NONBLOCK

// folderOnly opens a folder alone: anything else at the name is refused at the open, never opened,
// so a device there is not touched (ENOTDIR, which openedNotFolder knows).
const folderOnly = syscall.O_DIRECTORY

func openedNotFolder(err error) bool { return errors.Is(err, syscall.ENOTDIR) }

// uidOf is the owner of what fi describes.
func uidOf(fi fs.FileInfo) (int, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return -1, false
	}
	return int(st.Uid), true
}

// listedAs is nothing: a folder here has no list, fresh holds it to its owner by uid, and
// folderMode sets its mode, MkdirShared's 0o755 as Mkdir's perm.
func listedAs(*os.File, string, kind) error { return nil }

// heldPerm is nothing: a folder here holds any nine permission bits.
func heldPerm(fs.FileMode) error { return nil }

// newFolder makes part in cur, perm less the umask until folderMode sets it exactly. Every volume
// here keeps a mode, so listless is not read, and has no list, so the kind is not.
func newFolder(cur *os.Root, part string, perm fs.FileMode, _ kind, _ bool) error {
	return cur.Mkdir(part, perm)
}

// folderMode sets perm exactly on the folder f, keeping a setgid bit it took from the one above.
func folderMode(f *os.File, held fs.FileInfo, perm fs.FileMode) error {
	return f.Chmod(perm | held.Mode()&fs.ModeSetgid)
}

// flushRoot flushes the folder r is, so a folder made in it survives a power cut.
func flushRoot(r *os.Root) error {
	d, err := r.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}

// retried is op, tried once: nothing here refuses the removal of a file another process holds.
func retried(op func() error) error { return op() }

// heldPast is err: a removal here is never refused for a file held open.
func heldPast(err error) error { return err }

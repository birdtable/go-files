//go:build !windows

package write

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

const noWait = syscall.O_NONBLOCK // a pipe at the name answers at once, and its stat refuses it

func own(f *os.File, uid, gid int) error { return f.Chown(uid, gid) }

func flushOpen(d *os.File) error { return d.Sync() }

// idsOf is fi's owner and group, which every Unix stat gives.
func idsOf(fi fs.FileInfo) (uid, gid int, ok bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return -1, -1, false
	}
	return int(st.Uid), int(st.Gid), true
}

func mode(f *os.File, perm fs.FileMode) error { return f.Chmod(perm) }

func modeGiven() error { return nil }

// asked refuses nothing: a file here takes any mode, kept mode and owner (fill).
func asked(options, fs.FileMode) error { return nil }

// heldBy refuses nothing: a file here keeps the old one's mode and owner exactly (kept).
func heldBy(*os.Root, string, fs.FileInfo, options) error { return nil }

// irregular refuses nothing: a name here that is not a plain file has nothing to keep (kept).
func irregular(fs.FileInfo, options) error { return nil }

// readOnly is nothing: mode sets the whole mode here.
func readOnly(*os.File, fs.FileMode, options) error { return nil }

// stays is nothing: mode set the whole mode on the descriptor, and no remove here takes it off.
func stays(*os.Root, string, fs.FileMode, fs.FileInfo, options) error { return nil }

// indexed is Windows' alone: every Unix volume gives a file an inode number os.SameFile can trust,
// so told never refuses here.
func indexed(*os.File, options) (bool, error) { return true, nil }

// toldAt answers nil on Unix: indexed cannot refuse here, so a Swap opens neither name it moves,
// and one it may move but not read still swaps.
func toldAt(*os.Root, string, fs.FileInfo, options) error { return nil }

// forkHold takes syscall.ForkLock's read side until release, for Runnable: a fork takes its write
// side, so no child is forked holding a descriptor opened in between.
func forkHold() (release func()) {
	syscall.ForkLock.RLock()
	return syscall.ForkLock.RUnlock
}

// over renames tmp over name; the folder's flush after it makes the rename durable. It is
// sysOptions', which Windows reads.
func (sysOptions) over(root *os.Root, tmp, name string, _ *grip) error {
	return root.Rename(tmp, name)
}

// grip is the temp file's descriptor, opened again to read while it was still 0600 (again) and held
// from ready's compare to the end of place, so that no other file can have its inode number
// meanwhile: the rename goes by name, as write/README.md says, and NoReplace's link
// does too, whose placed name is read against the file written (linked).
type grip struct{ file *os.File }

// gripTemp takes held, which ready would close, as the grip: it is opened again by name by none, as
// a mode the writer may not read has been set on tmp by now.
func gripTemp(_ *os.Root, _ string, held *os.File, _ fs.FileInfo, _ options) (*grip, error) {
	return &grip{held}, nil
}

// keeps says g holds the descriptor ready was given, so ready does not close it.
func (g *grip) keeps() bool { return g != nil }

func (g *grip) close() error {
	if g == nil {
		return nil
	}
	return g.file.Close()
}

// named refuses nothing here: every step, the rename included, is os.Root's, which takes a name as
// written and refuses one that leads out. Windows refuses more (write_windows.go).
func named(*os.Root, string) error { return nil }

// logNamed refuses nothing here: os.Root takes a log's name as written. Windows strips a last
// dot or space (write_windows.go).
func logNamed(string) error { return nil }

func noLinks(err error) error { return err } // every Unix file system this runs on keeps hard links

// syncFolder is SyncFolder: a write's folder is flushed as any other is.
func syncFolder(root *os.Root, dir string) error { return SyncFolder(root, dir) }

func aside(root *os.Root, from, to string) error { return root.Rename(from, to) }

// pin is nothing on Unix: the count of names is read from the look the hold's check matched.
func pin(*os.Root, string, fs.FileInfo) (func() error, error) {
	return func() error { return nil }, nil
}

// sysOptions are options one system alone reads: none here.
type sysOptions struct{}

// forceNoIndex is fileid's test seam, which no volume here reads: every file has an inode.
func (sysOptions) forceNoIndex() bool { return false }

// newFile makes name beneath root by an exclusive create, 0600 before its mode is set, open as
// flag says: Shared's 0644 too is set on the descriptor after (fill). Every volume here keeps a
// mode, so no volume is refused.
func newFile(root *os.Root, name string, flag int, _ options) (*os.File, error) {
	return root.OpenFile(name, flag|os.O_CREATE|os.O_EXCL, 0o600)
}

// overOld changes nothing: under KeepMode the old file's mode is kept, and under KeepOwner alone
// Shared's 0644 is set with the old file's owner.
func overOld(o options) options { return o }

// move is Swap's move of from to to in root's folder, over nothing: a hard link, which refuses a
// name that is taken, then the removal of from; a removal that fails takes the link away again. It
// is never late: the folder's flush after the swap makes both moves durable.
func move(root *os.Root, from, to string, _ options) (late bool, err error) {
	if err := root.Link(from, to); err != nil {
		return false, err
	}
	if err := root.Remove(from); err != nil {
		return false, errors.Join(fmt.Errorf("%s was linked at %s, and its own name could not be removed", from, to), err, root.Remove(to))
	}
	return false, nil
}

// link hard-links tmp at name, over nothing, by the name: no call links a descriptor on every Unix,
// so a file put at tmp since the last check is the one linked, and linked takes it away again.
func (sysOptions) link(root *os.Root, tmp, name string, _ *grip) error { return root.Link(tmp, name) }

// rootOnPipes refuses nothing: only Windows has a pipes' device a root can be opened on.
func rootOnPipes(*os.Root, string) error { return nil }

// folderOnPipes refuses nothing: off Windows no path names a device or a pipe by its spelling, and
// a pipe at a name is refused by the open beneath the root.
func folderOnPipes(string, string) error { return nil }

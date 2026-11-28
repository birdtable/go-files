// Package write replaces a file whole, so a reader sees the old file or the new one and never half
// of either, and a power cut leaves one of the two. File and FileFrom go by a path, FileIn by a
// name beneath an os.Root. Append adds a line to a log kept under a cap. Keep makes a copy of a
// file at a name the caller gives, and PutBack puts one back. Swap replaces a program that may be
// running, the old one moved aside and put back on failure. The README says each step.
//
// Built on the standard recipe of a temp file beside the name, flushed and renamed over it, over
// os.Root and golang.org/x/sys. It adds the owner and mode set on the temp file's descriptor, the
// check that the file renamed has the device and inode number of the one written, the folder
// flushed (on Windows a rename written through), a log kept under a cap, and a copy kept and put
// back. The program configures the name, Mode or Shared, KeepMode, Owner, KeepOwner, Check, IfHolds, Gone,
// NoSync, NoReplace and Runnable, and for Append the cap, LogMode, Aside and its context. Windows' rename
// retry of about 600 ms, Append's tries and the temp file's name are fixed.
// It imports the standard library, golang.org/x/sys, confine and internal/fileid, and on Windows internal/winapi.
package write

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/internal/fileid"
)

// Option is one way a write differs from the plain one, which makes a file
// 0600 and its writer's, replaces what is at the name, and flushes. On
// Windows the file is made with an access list that only its writer's
// account and SYSTEM reach, and none of its folder's is inherited; a volume
// that keeps no access lists (FAT, exFAT) is refused with ErrNoAccessList.
type Option func(*options)

type options struct {
	perm      fs.FileMode
	modeSet   bool        // the caller gave Mode
	asked     fs.FileMode // Mode's perm as given, every bit, which Windows holds to the owner-only list
	replacing bool        // put writes it: File, FileFrom, FileIn and PutBack, and not Keep's copy
	copied    bool        // the mode is the source file's: Keep's copy and PutBack's, which keep its read-only attribute
	from      fs.FileInfo // the file a copy takes its mode from, whose group the group bits name (grouped)
	given     int         // with from, the group Owner gave the copy, which the group bits may name too
	uid, gid  int
	keepMode  bool
	keepOwner bool
	noReplace bool
	gone      bool // Gone: PutBackIn puts the copy only where nothing is, and every other write refuses it
	noSync    bool
	runnable  bool
	shared    bool // the caller gave Shared: on Windows cleared where the file is not made with its folder's list
	mixed     bool // the caller gave both Shared and Mode, which every write refuses before anything is made
	check     func(*os.File) error
	holds     *[sha256.Size]byte
	aside     string // Append's alone, by Aside
	// set by the package's tests alone (seam_test.go), read only under testing.Testing()
	step       func(at string)
	flush      func(root *os.Root, dir string) error
	closing    func(err error) error                  // a test's answer to the close of the grip, read only under testing.Testing()
	remove     func(root *os.Root, name string) error // a test's remove of the name linked just in place, in linked, and of the temp name after the link
	sysOptions                                        // what one system alone reads
}

// Mode is the file's mode, exactly: the umask does not narrow it.
//
// On Windows, where a file has an access list and no mode, the file keeps the
// owner-only access list Option says, which is narrower than perm for group
// and other, and perm's owner write bit is the read-only attribute, as
// os.Chmod sets it there: a perm without 0o200 makes the file read-only. A
// perm without owner read (0o400), or with any bit beyond the nine
// permission bits, setuid, setgid and sticky among them, is refused with
// ErrNoMode, as the owner-only list would give more than it asks.
// So Mode(0o644) there is still the owner-only list: a file
// other accounts read is written with Shared. Mode given with Shared, in
// either order, is refused with an error matching fs.ErrInvalid.
func Mode(perm fs.FileMode) Option {
	return func(o *options) { o.perm, o.asked, o.modeSet = perm&fs.ModePerm, perm, true }
}

// Shared writes a file other accounts read. On Unix it is 0644 exactly,
// whatever the umask, as Mode(0o644) makes it, and 0755 with Runnable.
//
// On Windows, where 0644 has no meaning, the temp file is made with no access
// list of its own, so it holds the inheritable entries of its folder and
// nothing else, as a file any program makes there does, and it is put in place
// as any write's is. Who reads and writes the file is then the folder's say,
// which can be wider or narrower than 0644. It is not read-only. A volume that
// keeps no access lists (FAT, exFAT) takes it, as there is no list to give:
// ErrNoAccessList is the plain write's refusal alone.
//
// Shared with Mode, in either order, is refused with an error matching
// fs.ErrInvalid before anything is made, on every system. With KeepMode, on
// Unix the old file's mode is kept, and 0644 is the mode where there is no old
// file; with KeepOwner alone the old file's owner is kept, with 0644. On
// Windows over an old plain file, under KeepMode or KeepOwner, the new file
// has the owner-only access list, as without Shared, and its folder's list
// only where there is no old file: a caller that wants the folder's list on
// every write gives Shared alone. So KeepMode with Shared over a file that
// holds its folder's list, as the first such write made, is refused with
// ErrNoMode when that list grants another account, as KeepMode refuses any
// old file that does. Keep and PutBack take it and, as Mode, do
// not apply it; Swap refuses it.
func Shared() Option { return func(o *options) { o.shared = true } }

// KeepMode keeps the mode of the plain file it replaces, when there is one;
// Mode's, Shared's, or 0600, when there is none.
//
// On Windows it keeps the old file's read-only attribute, and the new file has the owner-only
// access list Option says, not the old file's list. An old file whose list grants any account but
// the writer's, SYSTEM and Administrators is refused with ErrNoMode before anything is made, as the
// new file would lock that account out; with no file there it is not refused. That asks which
// accounts the list names, not their rights: the owner holds READ_CONTROL and WRITE_DAC on its own
// file whatever the list says. The list is read from the file at the name, never through a link.
// Keep refuses it with ErrNoMode, as it replaces no file.
func KeepMode() Option { return func(o *options) { o.keepMode = true } }

// KeepOwner gives the file the owner and group of the plain file it
// replaces, when there is one, before it is in place; Owner's, or the
// writer's, when there is none. On Unix a write that may not give the file
// to them (another account, or a group the writer is not in, without root)
// is refused and replaces nothing. The owner is read from the file at the name,
// never through a link, so an account that may make names in the folder
// can choose it, as it can the mode KeepMode keeps.
//
// On Windows, where a file made is its writer's, an old file whose owner is
// the writer's own account is kept exactly, and one another account owns, a
// group such as Administrators among them, is refused with ErrNoOwner before
// anything is made: the new file would be the writer's and lock the old owner
// out. With no file there it is not refused. Keep refuses it with ErrNoOwner,
// as it replaces no file.
func KeepOwner() Option { return func(o *options) { o.keepOwner = true } }

// Owner gives the file to uid and gid before it is in place, so it is never
// there as its writer's. -1 leaves either as it is. On Windows, which has no
// uid or gid, the file is its writer's: Owner(-1, -1), which asks for no
// change, is taken, and any other pair, a negative id included, is refused with ErrNoOwner.
func Owner(uid, gid int) Option { return func(o *options) { o.uid, o.gid = uid, gid } }

// NoReplace refuses to write over anything at the name, a link included,
// with an error matching fs.ErrExist. The file is put in place by a hard link
// from the temp file, so of two writers at once one wins. On Windows a mode
// without owner write leaves the placed name read-only, read and set again
// once it is in place, or the write's error says it is not.
func NoReplace() Option { return func(o *options) { o.noReplace = true } }

// NoSync flushes neither the file nor its folder. The file is still replaced
// whole; a power cut may leave the one before. On Windows, where a folder
// cannot be flushed and the rename is written through to the disk instead, it
// renames the file plainly.
func NoSync() Option { return func(o *options) { o.noSync = true } }

// Runnable writes a file that is run soon after. On Unix the temp file's
// create, its write, its flush and its close hold syscall.ForkLock's read
// side, which a fork of this process takes the write side of, so no child is
// forked holding the write descriptor: Linux and FreeBSD refuse to run a file
// open for writing (ETXTBSY) until every such child execs or exits. It holds
// only this process's own forks. A reader given to FileFrom is read whole into
// memory before the hold, so it may start a process or make a pipe or a socket,
// which would otherwise wait on the hold for ever. A file is 0700 when no Mode
// or KeepMode gives another, and 0755 with Shared. On Windows, where a child inherits only the
// handles made inheritable, nothing is held and the file has the owner-only
// access list Option says.
//
// To ask whether this system would run a file in a root, call confine.IsRunnable: on Unix it reads
// the exec bit that applies to this account, on Windows the extension against PATHEXT, and follows
// no link. exec.LookPath on an absolute path follows links, and on Windows asks only
// for a dot in the name, not PATHEXT.
func Runnable() Option { return func(o *options) { o.runnable = true } }

// Check runs on the temp file, read from its start through the read-only
// descriptor the write holds till its last check, opened while the written
// one was still open, before the file is put in place: an error leaves the
// old file and no temp file, and is the write's.
func Check(check func(*os.File) error) Option { return func(o *options) { o.check = check } }

// IfHolds writes only if the file at the name still holds what has this
// SHA-256; one that holds anything else, or is gone, is refused with
// ErrChanged. A writer that changes the file between the hash and the rename
// is not seen: the caller holds its own lock for that.
func IfHolds(sum [sha256.Size]byte) Option { return func(o *options) { o.holds = &sum } }

// Gone is PutBackIn's alone: it puts the copy back only where nothing is at
// the file's name, for a file that was removed since KeepIn, and holds must
// then be zero, as no file is there to hash. The copy is put in
// place as NoReplace puts a file, by a hard link from the temp file, never by
// a replace, with the copy's mode and Keep's group rule. Anything at the name,
// a file, a folder or a link, is refused with ErrChanged, and nothing is
// written over it or through it. A holds that is not zero, with Gone, is
// refused with an error matching fs.ErrInvalid, and so is Gone given to any
// other write, PutBack among them.
func Gone() Option { return func(o *options) { o.gone = true } }

var (
	// ErrNotDurable is a write whose new file is in place, but whose folder
	// could not be flushed. The write is not to be done again. On Windows it is
	// a volume that would not rename the file written through to the disk.
	ErrNotDurable = errors.New("is in place, but the folder's record of it could not be flushed to the disk, so a power cut now could bring back the file before")
	// ErrChanged is IfHolds's and PutBack's refusal of a file that does not
	// hold what the caller expects: nothing was written over it.
	ErrChanged = errors.New("does not hold what the caller expects: it changed since, so nothing was written over it")
	// ErrNoOwner is Owner's and KeepOwner's refusal where a file has no owner
	// of that kind (Windows): any Owner but (-1, -1), a KeepOwner of a
	// file another account owns, and Keep's KeepOwner. It matches
	// errors.ErrUnsupported.
	ErrNoOwner = fmt.Errorf("a file here has no owner by uid and gid, and is its writer's, so this owner cannot be given: %w", errors.ErrUnsupported)
	// ErrNoMode is Mode's, KeepMode's and LogMode's refusal where a file has
	// no mode (Windows, where a new file has the owner-only access list): a
	// Mode the list would give more than, a KeepMode of a file whose list
	// grants another account, Keep's KeepMode, and every LogMode. It matches
	// errors.ErrUnsupported.
	ErrNoMode = fmt.Errorf("a file here has an access list and no mode, and the owner-only list it is made with cannot give this mode: %w", errors.ErrUnsupported)
	// ErrNoAccessList is a write's refusal where Windows would not make its file this account's
	// alone: a volume that keeps no access lists (FAT, exFAT), one that will not say whether it
	// does, a share that will not take this account as the owner, or a folder where this account
	// cannot open by name what it makes there with that list, as under a restricted token or an
	// AppContainer the list does not name, or on a share that knows this account by another SID,
	// which a throwaway file made and removed first finds. The cause follows the text, and a
	// denial matches fs.ErrPermission too. Nothing is made. It matches errors.ErrUnsupported.
	ErrNoAccessList = fmt.Errorf("the volume keeps no access lists, so a file made here would not be this account's alone, and nothing was made: %w", errors.ErrUnsupported)
	// errNotThrough is the rename's error on a Windows volume that refuses to
	// write it through: the file is renamed plainly, and a write wraps it in
	// ErrNotDurable.
	errNotThrough = errors.New("the volume would not rename the file written through to the disk, so it was renamed without")
	// errTempLeft is place's error under NoReplace when the file is linked at its name and the temp
	// name could not be removed: the file is in place, so the rest of the write runs.
	errTempLeft = errors.New("its temp file is left")
	// ErrNoHardLinks is NoReplace's refusal on Windows on a volume that keeps
	// access lists and no hard links (some SMB servers): nothing was put in
	// place. FAT and exFAT, which keep neither, are refused first, with
	// ErrNoAccessList.
	ErrNoHardLinks = errors.New("the volume keeps no hard links, which NoReplace puts a file in place by, so nothing was written")
	// ErrNoFileIndex is a write's, Swap's or Append's refusal on Windows of a
	// file whose 64-bit file index reads zero or all-ones, which os.SameFile
	// needs to tell it apart from another put in its place. A zero index is a
	// volume's answer: every file on a volume that keeps no file IDs (a WebDAV
	// drive, some SMB servers) reads zero (Go 1.27.4 os/types_windows.go does
	// not test for zero). An all-ones index is one file's: ReFS, whose real file
	// ID is 128-bit, returns it for some files. FAT32 answers a non-zero
	// directory-entry index (measured on one FAT32 volume) and is refused
	// earlier for want of access lists; exFAT was not measured. It is
	// internal/fileid's one value, which confine, lock, appdirs, treesum and
	// trust refuse with too, and matches errors.ErrUnsupported.
	ErrNoFileIndex = fileid.ErrNoFileIndex
)

// File replaces path with content. On Windows a rename another process holds
// back for a moment, a scanner or an indexer, is tried again for about
// 600 ms: the window is a fact of the system, not the caller's, and a caller
// that must answer sooner has no option to shorten it.
func File(path string, content []byte, opts ...Option) error {
	return FileFrom(path, bytes.NewReader(content), opts...)
}

// FileFrom replaces path with what src gives until io.EOF. An error from src
// leaves the old file. On Windows its rename is tried again for about 600 ms,
// as File's is.
func FileFrom(path string, src io.Reader, opts ...Option) error {
	root, err := openFolder(filepath.Dir(path), path)
	if err != nil {
		return err
	}
	err = put(root, filepath.Base(path), src, settle(opts))
	if err := errors.Join(err, root.Close()); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// FileIn replaces name, beneath root, with content, as File does. Every step
// goes through root, which refuses a name, or a link or folder swapped in on
// the way, that leads out of it; a link at name itself is replaced. On Windows
// its rename is tried again for about 600 ms, as File's is.
func FileIn(root *os.Root, name string, content []byte, opts ...Option) error {
	if err := rootOnPipes(root, name); err != nil {
		return fmt.Errorf("%s beneath %s: %w", name, root.Name(), err)
	}
	if err := put(root, name, bytes.NewReader(content), settle(opts)); err != nil {
		return fmt.Errorf("%s beneath %s: %w", name, root.Name(), err)
	}
	return nil
}

// openFolder opens dir, the folder of path, as the root a call of write that takes a path works
// through. dir and path are refused before any open where either names a device or a pipe or is
// on a host's pipes share (pipesPath), and the root opened where its
// folder is a host's pipes (rootOnPipes), before any name beneath it is opened, so each open
// beneath it is of a name in a folder on a disk. A refusal is an *fs.PathError, op "open",
// matching fs.ErrInvalid.
func openFolder(dir, path string) (*os.Root, error) {
	for _, p := range []string{dir, path} {
		if pipesPath(p) {
			return nil, &fs.PathError{Op: "open", Path: p, Err: fmt.Errorf("the path names a device or a pipe, or a host's pipes share, and nothing was opened: %w", fs.ErrInvalid)}
		}
	}
	root, err := os.OpenRoot(dir) //nolint:forbidigo // the caller's folder, named as no device, pipe or pipes share; Go's open passes no quality of service, and a folder it reaches on a host's pipes, through a link, is refused next, before any open beneath it
	if err != nil {
		return nil, err
	}
	if err := rootOnPipes(root, path); err != nil {
		return nil, errors.Join(err, root.Close())
	}
	return root, nil
}

func settle(opts []Option) options {
	o := options{perm: 0o600, uid: -1, gid: -1}
	for _, opt := range opts {
		opt(&o)
	}
	o.mixed = o.shared && o.modeSet
	switch {
	case o.modeSet:
	case o.shared && o.runnable:
		o.perm = 0o755
	case o.shared:
		o.perm = 0o644
	case o.runnable:
		o.perm = 0o700
	}
	return o
}

// errGone is the refusal of Gone given to any write but PutBackIn: PutBack, File, FileFrom,
// FileIn, Keep, KeepIn and Swap.
var errGone = fmt.Errorf("Gone is PutBackIn's alone, which puts back a copy where nothing is: %w", fs.ErrInvalid)

// errMixed is the refusal of Shared given with Mode, in either order: one says the mode by intent and
// the other by number, and neither wins.
var errMixed = fmt.Errorf("Shared and Mode were both given, and a write takes one of the two: %w", fs.ErrInvalid)

func put(root *os.Root, name string, src io.Reader, o options) (err error) {
	if o.mixed {
		return errMixed
	}
	if o.gone {
		return errGone
	}
	if err := named(root, name); err != nil {
		return err
	}
	if o.holds != nil {
		if err := still(root, name, *o.holds, o); err != nil {
			return err
		}
	}
	o.replacing = true
	perm, o, err := kept(root, name, o)
	if err != nil {
		return err
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	tmp := filepath.Join(filepath.Dir(name), "."+filepath.Base(name)+"."+hex.EncodeToString(b)+".tmp")
	made, held, err := create(root, tmp, src, perm, o)
	if !made {
		return err
	}
	placed := false
	defer func() {
		if !placed {
			err = errors.Join(err, root.Remove(tmp))
		}
	}()
	if err != nil {
		return err
	}
	o.reached("closed")
	mine, g, err := ready(root, tmp, held, o)
	if err != nil {
		return err
	}
	o.reached("written")
	placed, err = place(root, tmp, name, mine, g, o)
	said, err := settled(placed, name, err, shut(g, o))
	if err != nil {
		return err
	}
	if said != nil {
		defer func() { err = errors.Join(err, said) }()
	}
	o.reached("placed")
	if err := stays(root, name, perm, mine, o); err != nil {
		return err
	}
	if o.noSync {
		return nil
	}
	flush := syncFolder
	if o.flush != nil && testing.Testing() {
		flush = o.flush
	}
	if err := flush(root, filepath.Dir(name)); err != nil {
		return fmt.Errorf("%s %w: %w", name, ErrNotDurable, err)
	}
	return nil
}

// kept is the new file's mode, and o with the owner it is given: under KeepMode the mode of the
// plain file at name, or else Mode's or 0600; under KeepOwner that file's owner, where it has one
// by uid and gid, or else Owner's. What cannot be read, for any reason but that nothing is there,
// is the write's error, not a guess. Where the new file cannot keep what the old one has (heldBy,
// irregular, Windows), it is refused before anything is made. Over a plain file kept from, Shared is
// as overOld says: Windows makes the new file with the owner-only list, as without Shared.
func kept(root *os.Root, name string, o options) (fs.FileMode, options, error) {
	if !o.keepMode && !o.keepOwner {
		return o.perm, o, nil
	}
	fi, err := root.Lstat(name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return o.perm, o, nil
	case err != nil:
		return 0, o, err
	case !fi.Mode().IsRegular():
		return o.perm, o, irregular(fi, o)
	}
	o.reached("kept")
	if err := heldBy(root, name, fi, o); err != nil {
		return 0, o, err
	}
	o = overOld(o)
	perm := o.perm
	if o.keepMode {
		perm, o.from = fi.Mode().Perm(), nil // the old file's mode, which names no copy's group
	}
	if uid, gid, ok := idsOf(fi); ok && o.keepOwner {
		o.uid, o.gid = uid, gid
	}
	return perm, o, nil
}

// SyncFolder flushes the folder dir beneath root, so a change in it that
// no File or FileIn made, a removal, a file made by an exclusive create or
// a folder made, survives a power cut. A dir that is missing, not a
// folder, or reached through a link out of root is an error on every
// system. On Windows the folder is opened again by its own handle, to
// write, and flushed; a volume that will not flush it is an error matching
// errors.ErrUnsupported, and a folder this account may not write to one
// matching fs.ErrPermission. The open never waits: a pipe at dir is
// refused at once as not a folder.
func SyncFolder(root *os.Root, dir string) error {
	if err := rootOnPipes(root, dir); err != nil {
		return fmt.Errorf("%s beneath %s: %w", dir, root.Name(), err)
	}
	d, err := root.OpenFile(dir, os.O_RDONLY|noWait, 0) // a pipe at dir answers at once
	if err == nil {
		err = errors.Join(folderFlush(d), d.Close())
	}
	if err != nil {
		return fmt.Errorf("%s beneath %s: %w", dir, root.Name(), err)
	}
	return nil
}

// folderFlush flushes d, refusing what is not a folder.
func folderFlush(d *os.File) error {
	fi, err := d.Stat()
	switch {
	case err != nil:
		return err
	case !fi.IsDir():
		return errors.New("is not a folder")
	}
	return flushOpen(d)
}

// create makes tmp by an exclusive create, opens it again to read while it is still 0600 and the
// writer's, checks that is the file made, fills it and closes it, and answers whether it made it
// and the read descriptor, so no other file can have its inode number while held: closed here if
// the write fails. Under Runnable all of it holds the fork lock
// (forkHold), and nothing in it starts a process, or the lock would wait on itself.
func create(root *os.Root, tmp string, src io.Reader, perm fs.FileMode, o options) (made bool, held *os.File, err error) {
	if o.runnable {
		if _, whole := src.(*bytes.Reader); !whole {
			// Read first: a reader that starts a process, or on macOS makes a pipe or a socket,
			// would wait under the hold for a fork that waits for it.
			b, err := io.ReadAll(src)
			if err != nil {
				return false, nil, err
			}
			src = bytes.NewReader(b)
		}
		defer forkHold()()
	}
	f, err := newFile(root, tmp, os.O_WRONLY, o)
	if err != nil {
		return false, nil, err
	}
	o.reached("temp")
	if held, err = again(root, tmp, f, o); err == nil {
		err = fill(f, src, perm, o)
	}
	if err = errors.Join(err, f.Close()); err != nil && held != nil {
		return true, nil, errors.Join(err, held.Close())
	}
	return true, held, err
}

// again opens tmp to read while f, which made it, is open, and refuses it unless it is f's file.
func again(root *os.Root, tmp string, f *os.File, o options) (*os.File, error) {
	mine, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if err := told(tmp, f, o); err != nil {
		return nil, err // a no-index volume cannot tell tmp apart, so fill is not called and Check is not reached
	}
	r, err := root.OpenFile(tmp, os.O_RDONLY|noWait, 0)
	if err != nil {
		return nil, err
	}
	got, err := r.Stat()
	if err == nil && !os.SameFile(mine, got) {
		err = swapped(tmp)
	}
	if err != nil {
		return nil, errors.Join(err, r.Close())
	}
	return r, nil
}

// ready runs Check on held outside the fork lock, so a Check may start a process, refuses a file
// put at tmp since, grips tmp for the rename where the rename goes by a handle (gripTemp, Windows),
// closes held, and answers what held is and the grip: till held closes no other file can have its
// inode number, and from there the grip pins it to the rename. Without a grip only stays compares
// after. held is closed here, as on Windows an open handle stops a rename of its folder, unless the grip keeps it (Unix); the grip
// is open only to the rename.
func ready(root *os.Root, tmp string, held *os.File, o options) (mine fs.FileInfo, g *grip, err error) {
	defer func() {
		if !g.keeps() {
			err = errors.Join(err, held.Close())
		}
		if err != nil {
			err = errors.Join(err, g.close())
			mine, g = nil, nil
		}
	}()
	if o.check != nil {
		if err := o.check(held); err != nil {
			return nil, nil, err
		}
	}
	mine, err = held.Stat()
	if err != nil {
		return nil, nil, err
	}
	at, err := root.Lstat(tmp)
	if err == nil && !os.SameFile(mine, at) {
		err = swapped(tmp)
	}
	// No told here: again (create) refuses a no-index volume on this same temp file, on every write
	// path, before ready is reached, so ready's compare is a second look at a file already told
	// apart.
	if err != nil {
		return nil, nil, err
	}
	o.reached("gripping")
	g, err = gripTemp(root, tmp, held, mine, o)
	return mine, g, err
}

func swapped(tmp string) error {
	return fmt.Errorf("%s was swapped for another file while it was written, so it was not put in place", tmp)
}

// told refuses when the file f is a handle to has no file index os.SameFile can tell it apart by: a
// zero index, which a volume with no file IDs gives every file, or an all-ones one, which ReFS gives
// some files. Without one a compare passes any two files, so the write would not see one put in its
// place (ErrNoFileIndex). On Unix every file has an inode os.SameFile can trust, so
// indexed never refuses there.
func told(name string, f *os.File, o options) error {
	switch ok, err := indexed(f, o); {
	case err != nil:
		return err
	case !ok:
		return fmt.Errorf("%s: %w", name, ErrNoFileIndex)
	}
	return nil
}

// bothTold refuses a no-index file for both names a Swap moves, before it moves either, each
// against the look Swap took of it (old, nu). toldAt is one system's (write_windows.go, write_unix.go).
func bothTold(root *os.Root, a string, aInfo fs.FileInfo, b string, bInfo fs.FileInfo, o options) error {
	return errors.Join(toldAt(root, a, aInfo, o), toldAt(root, b, bInfo, o))
}

// place puts tmp at name, by a hard link under NoReplace (link: by name on Unix, and by g on Windows),
// or else by a rename that goes by g where
// there is one, and answers whether it is there: a rename the volume would not write through is,
// with errNotThrough.
func place(root *os.Root, tmp, name string, mine fs.FileInfo, g *grip, o options) (bool, error) {
	if !o.noReplace {
		err := o.over(root, tmp, name, g)
		return err == nil || errors.Is(err, errNotThrough), err
	}
	if err := o.link(root, tmp, name, g); err != nil {
		return false, refusedLink(root, tmp, mine, noLinks(err))
	}
	if err := linked(root, tmp, name, mine, o); err != nil {
		return false, err
	}
	if err := take(root, tmp, o); err != nil {
		return true, fmt.Errorf("%s is in place, and %w: %w", name, errTempLeft, err)
	}
	return true, nil
}

// linked reads name, just linked from tmp, against mine, the file written. On Unix the link went by
// the temp name, so a file put there since the last check is the one at name now: it is taken away,
// and the write refused as swapped, but only while name still shows that file, so a name another
// process made since is left. On Windows the link went by the grip and only the file written is
// linked, so this finds a file renamed over the name since. A remove that fails is joined to the refusal.
// The second look compares two looks at the name, neither of a file that was told, and two no-index
// files compare as one: so the file at name is told against the first look (toldAt, Windows) before
// it is taken, and one with no index is left, the refusal joined to ErrNoFileIndex.
func linked(root *os.Root, tmp, name string, mine fs.FileInfo, o options) error {
	at, err := root.Lstat(name)
	if err != nil {
		return fmt.Errorf("%s was linked, and could not be read to check it: %w", name, err)
	}
	if os.SameFile(mine, at) {
		return nil
	}
	refusal := swapped(tmp)
	o.reached("taking")
	if now, err := root.Lstat(name); err != nil || !os.SameFile(at, now) {
		return refusal
	}
	if err := toldAt(root, name, at, o); errors.Is(err, fs.ErrNotExist) {
		return refusal
	} else if err != nil {
		return errors.Join(refusal, fmt.Errorf("%s may hold the swapped file, and is left: %w", name, err))
	}
	if err := take(root, name, o); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errors.Join(refusal, fmt.Errorf("%s holds the swapped file, and its name could not be removed: %w", name, err))
	}
	return refusal
}

// fill gives f its owner, then src, then perm, all on the descriptor, and flushes it. A Mode,
// KeepMode or KeepOwner this system cannot give (asked, Windows) is refused first, as Owner is:
// KeepOwner with no file to keep from asks own for -1 and -1, which changes nothing.
// Where a file has an attribute for the owner's write bit, a mode the caller asked for, or a copy's, sets it
// (readOnly, Windows). A copy's group bits stay only where f, owned, is in its source's group (grouped).
func fill(f *os.File, src io.Reader, perm fs.FileMode, o options) error {
	if err := asked(o, perm); err != nil {
		return err
	}
	if o.uid >= 0 || o.gid >= 0 || o.keepOwner {
		if err := own(f, o.uid, o.gid); err != nil {
			return err
		}
	}
	if o.from != nil {
		var err error
		if perm, err = grouped(f, perm, o.from, o.given); err != nil {
			return err
		}
	}
	if _, err := io.Copy(f, src); err != nil {
		return err
	}
	if err := readOnly(f, perm, o); err != nil {
		return err
	}
	if err := mode(f, perm); err != nil || o.noSync {
		return err
	}
	return f.Sync()
}

// grouped is perm, a copy's mode taken from src, with its group bits cleared unless f, the new
// file, is in src's group, or in given, the group the caller gave it by Owner: the bits name src's
// group, and a copy in another folder may be given that folder's group, or the writer's, whose
// members they would let read what only src's could. Nothing is chowned to make the
// groups match: Owner alone gives a group, and is given before this look. Others' bits are not
// touched. A file with no group (Windows) is unchanged.
func grouped(f *os.File, perm fs.FileMode, src fs.FileInfo, given int) (fs.FileMode, error) {
	_, from, ok := idsOf(src)
	if !ok || perm&0o070 == 0 {
		return perm, nil
	}
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if _, to, ok := idsOf(fi); ok && (to == from || given >= 0 && to == given) {
		return perm, nil
	}
	return perm &^ 0o070, nil
}

func still(root *os.Root, name string, sum [sha256.Size]byte, o options) error {
	f, _, err := regular(root, name, o)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %w", ErrChanged, err)
	}
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(h, f)
	if err := errors.Join(err, f.Close()); err != nil {
		return err
	}
	if [sha256.Size]byte(h.Sum(nil)) != sum {
		return ErrChanged
	}
	return nil
}

// regular opens name to read: a plain file by the look, and by the
// descriptor's own stat the same plain file, since Linux gives a freed inode
// number to the next file made. The open never waits on a pipe (noWait).
func regular(root *os.Root, name string, o options) (*os.File, fs.FileInfo, error) {
	fi, err := root.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%s is %v, not a plain file, and only one is read", name, fi.Mode().Type())
	}
	o.reached("look")
	f, err := root.OpenFile(name, os.O_RDONLY|noWait, 0)
	if err != nil {
		return nil, nil, err
	}
	got, err := f.Stat()
	if err == nil {
		if err = told(name, f, o); err == nil && (!got.Mode().IsRegular() || !os.SameFile(got, fi)) {
			err = fmt.Errorf("%s changed between its look and its open, and was not read", name)
		}
	}
	if err != nil {
		return nil, nil, errors.Join(err, f.Close())
	}
	return f, got, nil
}

func (o options) reached(at string) {
	if o.step != nil && testing.Testing() {
		o.step(at)
	}
}

// shut closes g, as a test's closeWith answers it under testing.
func shut(g *grip, o options) error {
	err := g.close()
	if o.closing != nil && testing.Testing() {
		err = o.closing(err)
	}
	return err
}

func inPlace(name string, closed error) error {
	return fmt.Errorf("%s is in place, and the handle held on its temp file did not close: %w", name, closed)
}

// settled reads place's answer and the close of what was held on the temp file. Where the file is in
// place, a temp name that could not be removed and a close that failed each say so in said, which
// put joins to its answer once the rest of the write has run, the read-only attribute and the
// folder's flush among it; any other failure is the write's error now.
func settled(placed bool, name string, err, closed error) (said, now error) {
	if !placed || err != nil && !errors.Is(err, errTempLeft) {
		return nil, refused(placed, name, errors.Join(err, closed))
	}
	if closed != nil {
		closed = inPlace(name, closed)
	}
	return errors.Join(err, closed), nil
}

// refused is put's answer after place: a rename not written through, in place, is ErrNotDurable.
func refused(placed bool, name string, err error) error {
	if placed && errors.Is(err, errNotThrough) {
		return fmt.Errorf("%s %w: %w", name, ErrNotDurable, err)
	}
	return err
}

// take removes name, a name linked just in place or the temp name after the link, as a test's
// removeWith answers it under testing.
func take(root *os.Root, name string, o options) error {
	if o.remove != nil && testing.Testing() {
		return o.remove(root, name)
	}
	return root.Remove(name)
}

// refusedLink words a link that failed: where tmp no longer shows the file written, it was swapped,
// as on Windows, where the link by the grip is refused once a swap took the file's last name.
func refusedLink(root *os.Root, tmp string, mine fs.FileInfo, err error) error {
	if at, lerr := root.Lstat(tmp); lerr != nil || !os.SameFile(mine, at) {
		return errors.Join(swapped(tmp), err)
	}
	return err
}

package write

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/birdtable/go-files/internal/fileid"
	"github.com/birdtable/go-files/internal/winapi"
)

const noWait = 0 // an open of a name never waits for a writer here

// own changes nothing: a file here has no uid or gid, and is its writer's. asked has refused every
// owner but -1 and -1, which ask for no change, as KeepOwner asks them.
func own(*os.File, int, int) error { return nil }

// flushOpen flushes the folder open at d, as go-run's runlog flushes one: Windows flushes a folder
// only through a handle open to write, which it gives a folder only with backup semantics, so d is
// opened again by its own handle, the same folder whatever was renamed since, and FlushFileBuffers
// flushes that. What the volume will not do, at the open or the flush, matches
// errors.ErrUnsupported; any other error is itself. Nothing answers nil unflushed.
func flushOpen(d *os.File) error { return flushSeen(d, nil) }

// flushSeen is flushOpen, whose flush, under testing.Testing() alone, is seen's: seen is handed the
// handle open to write and FlushFileBuffers' error, and answers the error flushOpen takes.
func flushSeen(d *os.File, seen func(h windows.Handle, flushed error) error) (err error) {
	h, err := winapi.OpenAt(windows.Handle(d.Fd()), "", winapi.How{Access: windows.GENERIC_WRITE, Share: windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE,
		Disposition: windows.FILE_OPEN, Options: windows.FILE_DIRECTORY_FILE | windows.FILE_OPEN_FOR_BACKUP_INTENT})
	if err != nil {
		return unflushed(winapi.Dos(err))
	}
	defer func() { err = errors.Join(err, windows.CloseHandle(h)) }()
	flush := windows.FlushFileBuffers
	if seen != nil && testing.Testing() {
		flush = func(h windows.Handle) error { return seen(h, windows.FlushFileBuffers(h)) }
	}
	return unflushed(flush(h))
}

// unflushed is err, matching errors.ErrUnsupported where it is the volume's refusal of the call, as
// FAT may answer a folder's flush.
func unflushed(err error) error {
	if errors.Is(err, windows.ERROR_INVALID_FUNCTION) || errors.Is(err, windows.ERROR_NOT_SUPPORTED) || errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return fmt.Errorf("the volume would not flush the folder: %w: %w", errors.ErrUnsupported, err)
	}
	return err
}

func idsOf(fs.FileInfo) (uid, gid int, ok bool) { return -1, -1, false } // a SID, which heldBy holds to the writer's

func mode(*os.File, fs.FileMode) error { return nil } // a new file has the owner-only list it was made with (newFile)

func modeGiven() error { return ErrNoMode } // LogMode's, which is refused whatever its bits

// asked refuses, with ErrNoMode, a Mode the owner-only list would give more than: one without owner
// read, or with a bit beyond the nine permission bits, read from Mode's perm as given, as setuid,
// setgid and sticky are; and a perm without owner read. A KeepMode or KeepOwner where no file is
// replaced, as Keep's copy, has nothing to keep and nothing heldBy read, and is refused with
// ErrNoMode or ErrNoOwner. An Owner of any ids but exactly -1 and -1, a negative one included, is
// refused with ErrNoOwner, as a file here has no uid or gid.
func asked(o options, perm fs.FileMode) error {
	switch {
	case o.uid != -1 || o.gid != -1:
		return fmt.Errorf("uid %d and gid %d, where only -1 and -1, which ask for no change, are taken: %w", o.uid, o.gid, ErrNoOwner)
	case o.keepOwner && !o.replacing:
		return fmt.Errorf("KeepOwner where no file is replaced: %w", ErrNoOwner)
	case o.keepMode && !o.replacing:
		return fmt.Errorf("KeepMode where no file is replaced: %w", ErrNoMode)
	case o.modeSet && (o.asked&^fs.ModePerm != 0 || o.asked&0o400 == 0):
		return fmt.Errorf("the mode %#o has no owner read, or bits beyond the nine permission bits, which the owner-only list would give more than: %w", uint32(o.asked), ErrNoMode)
	case (o.modeSet || o.keepMode) && perm&0o400 == 0:
		return fmt.Errorf("the mode %#o has no owner read, which the owner-only list would give more than: %w", uint32(perm), ErrNoMode)
	}
	return nil
}

// readOnly sets the read-only attribute on f when perm has no owner write (0o200), as os.Chmod
// maps it, and the mode is asked for: by Mode or KeepMode of a file put at a name, or by a copy,
// Keep's of the file it copies and PutBack's of kept, Mode given or not, as Unix gives both their
// source's mode. perm is what Unix sets: Mode's, the old file's under
// KeepMode, the copied file's in Keep and kept's in PutBack. A perm with owner write changes
// nothing, as a new file is not read-only.
func readOnly(f *os.File, perm fs.FileMode, o options) error {
	if !o.copied && (!o.replacing || !o.modeSet && !o.keepMode) || perm&0o200 != 0 {
		return nil
	}
	return f.Chmod(perm)
}

// stays reads the read-only attribute readOnly set on the file NoReplace placed at name, and sets
// it again where it is gone: the remove of the temp name, where a volume will not delete a
// read-only file as is, clears the attribute first, on the file both names share (Go's Deleteat).
// name is opened beneath root, following no link, and must be mine, the
// file written; what cannot be read or set is the write's error, never nil over a writable file.
func stays(root *os.Root, name string, perm fs.FileMode, mine fs.FileInfo, o options) (err error) {
	if !o.noReplace || !o.copied && !o.modeSet && !o.keepMode || perm&0o200 != 0 {
		return nil
	}
	d, err := root.Open(filepath.Dir(name))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	h, err := winapi.OpenAt(windows.Handle(d.Fd()), filepath.Base(name), winapi.How{Access: windows.FILE_READ_ATTRIBUTES | windows.FILE_WRITE_ATTRIBUTES,
		Share: windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE, Disposition: windows.FILE_OPEN, Attributes: windows.OBJ_CASE_INSENSITIVE})
	if err != nil {
		return fmt.Errorf("%s is in place, and its read-only attribute could not be read: %w", name, winapi.Dos(err))
	}
	f := os.NewFile(uintptr(h), name)
	defer func() { err = errors.Join(err, f.Close()) }()
	got, err := f.Stat()
	if err != nil {
		return fmt.Errorf("%s is in place, and its read-only attribute could not be read: %w", name, err)
	}
	if ok, ierr := indexed(f, o); ierr != nil {
		return fmt.Errorf("%s is in place, and its file index could not be read: %w", name, ierr)
	} else if !ok {
		return fmt.Errorf("%s is in place, and whether it is still the file written is not known: %w", name, ErrNoFileIndex)
	}
	switch {
	case !os.SameFile(got, mine):
		return fmt.Errorf("%s was swapped for another file once it was in place, so its read-only attribute is not known", name)
	case got.Mode().Perm()&0o200 == 0:
		return nil
	}
	if err := f.Chmod(perm); err != nil {
		return fmt.Errorf("%s is in place and writable, where its mode %#o has no owner write: %w", name, uint32(perm), err)
	}
	return nil
}

// irregular refuses, before anything is made, a KeepMode or KeepOwner over a name that is neither
// a plain file nor a link, by kept's look: a junction, a reparse point of a driver's own tag that
// Go reads as irregular, a folder or a socket has no mode or owner heldBy could read and the new
// file meet, so it is refused with ErrNoMode or ErrNoOwner, never passed through.
// A link is replaced as on every system.
func irregular(fi fs.FileInfo, o options) error {
	switch {
	case fi.Mode().Type() == fs.ModeSymlink:
		return nil
	case o.keepOwner:
		return fmt.Errorf("KeepOwner over a name that is not a plain file (%v): %w", fi.Mode().Type(), ErrNoOwner)
	}
	return fmt.Errorf("KeepMode over a name that is not a plain file (%v): %w", fi.Mode().Type(), ErrNoMode)
}

// heldBy refuses, before anything is made, a KeepMode or KeepOwner the new file cannot meet: fi is
// the plain file at name, by kept's look. That file is opened beneath root by its last name in its
// folder's handle, following no link, to read its descriptor alone, and must still be fi's. Under
// KeepMode, an old file whose access list grants any account but this one, SYSTEM and
// Administrators (winapi.Narrow) is refused with ErrNoMode: the new file's owner-only list would
// lock that account out. Under KeepOwner, an old file another account owns, a group among them
// (winapi.Mine), is refused with ErrNoOwner: the new file is this account's.
func heldBy(root *os.Root, name string, fi fs.FileInfo, o options) (err error) {
	if !o.keepMode && !o.keepOwner {
		return nil
	}
	d, err := root.Open(filepath.Dir(name))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	h, err := winapi.OpenAt(windows.Handle(d.Fd()), filepath.Base(name), winapi.How{Access: windows.READ_CONTROL | windows.FILE_READ_ATTRIBUTES,
		Share: windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE, Disposition: windows.FILE_OPEN, Attributes: windows.OBJ_CASE_INSENSITIVE})
	if err != nil {
		return winapi.Dos(err)
	}
	f := os.NewFile(uintptr(h), name)
	defer func() { err = errors.Join(err, f.Close()) }()
	got, err := f.Stat()
	if err == nil && !os.SameFile(got, fi) {
		err = fmt.Errorf("%s changed between its look and its open, and nothing was written", name)
	}
	if err != nil {
		return err
	}
	if err := told(name, f, o); err != nil { // a no-index volume cannot tell the kept file apart
		return err
	}
	if o.keepMode {
		narrow, err := winapi.Narrow(h)
		if err != nil {
			return err
		}
		if !narrow {
			return fmt.Errorf("%s grants an account beyond this one, SYSTEM and Administrators, which the new file's owner-only list would lock out, so its mode cannot be kept: %w", name, ErrNoMode)
		}
	}
	if o.keepOwner {
		mine, err := winapi.Mine(h, o.elsewhere)
		if err != nil {
			return err
		}
		if !mine {
			return fmt.Errorf("%s is another account's, and the new file would be this account's, so its owner cannot be kept: %w", name, ErrNoOwner)
		}
	}
	return nil
}

// forkHold holds nothing: a child here inherits only the handles made inheritable, and a write's
// are not, so Runnable takes no lock.
func forkHold() (release func()) { return func() {} }

// over renames tmp over name by g, the handle gripTemp opened at tmp and compared with the file
// written, making a read-only old file writable for the rename, and read-only again if it fails.
// Unless NoSync, g was opened to write the rename through to the disk, which stands for the folder
// flush Windows has no call for; on a volume that refused that, g was opened without it, and the
// rename is errNotThrough with the file in place. Every rename goes by g, with NoSync and on a
// refusing volume too, so a file put at the temp name is never renamed on any path.
func (sysOptions) over(root *os.Root, _, name string, g *grip) error {
	fi, err := root.Lstat(name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err // whether the old file is read-only is not known, so it is not renamed over
	}
	readOnly := err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o200 == 0
	if readOnly {
		if err := root.Chmod(name, 0o600); err != nil {
			return err
		}
	}
	// The folder is opened beneath root again at each try, as the one a try before it opened may have
	// been moved away since: the file goes into the folder name leads to, or the open refuses.
	err = winapi.Retried(func() error {
		d, err := root.Open(filepath.Dir(name))
		if err != nil {
			return err
		}
		return errors.Join(winapi.Rename(windows.Handle(g.file.Fd()), windows.Handle(d.Fd()), filepath.Base(name), true), d.Close())
	})
	if err == nil && g.late != nil {
		err = g.late
	}
	if err != nil && !errors.Is(err, errNotThrough) && readOnly {
		err = errors.Join(err, root.Chmod(name, fi.Mode().Perm()))
	}
	return err
}

// named refuses, before a write opens anything, a name that os.Root's rename would not take as
// written: over hands its last element to NtSetInformationFile by winapi.Rename on the grip, which has none of the
// Win32 name rules os.Root's calls go by, so one name could mean two files. A name that is not
// local, a device name such as NUL among them, is os.Root's own refusal, in its words, as on every
// system. A name that is not clean (a trailing or doubled separator, a . or a ..) or has a component
// ending in a dot or a space, which the Win32 rules strip, is refused with an error matching
// fs.ErrInvalid: given app.toml., os.Root's Lstat, hash and chmod would reach app.toml, and the
// rename would write app.toml. beside it. confine's renames refuse the same.
func named(root *os.Root, name string) error {
	if !filepath.IsLocal(name) {
		if _, err := root.Lstat(name); err != nil {
			return err
		}
		return fmt.Errorf("it is not a local name: %w", fs.ErrInvalid) // os.Root took it: not seen in Go 1.27.4
	}
	n := filepath.FromSlash(name)
	if filepath.Clean(name) != n || slices.ContainsFunc(strings.Split(n, `\`), func(p string) bool { return strings.HasSuffix(p, ".") || strings.HasSuffix(p, " ") }) {
		return fmt.Errorf("it is not a clean name, or a component of it ends in a dot or a space, which the Win32 name rules strip, so it would name one file to os.Root and another to the rename: %w", fs.ErrInvalid)
	}
	return nil
}

// logNamed is Append's door: a last name ending in a dot or a space is winapi.ErrStrippedName, as
// os.Root's look would find the log at the stripped name, and the make would make the name as
// written, so the look never matches what it holds.
func logNamed(name string) error { return winapi.Stripped(name) }

// grip is the temp file's rename handle and its folder's. gripTemp opens it and compares it with
// the file written while ready still holds that file, and it stays open to the rename (over), so
// it is the pin from there: no other file can take the written file's record while it is open.
type grip struct {
	dir, file *os.File
	late      error // the volume refused write-through, so file was opened without it: errNotThrough
}

// close closes g's handles; a nil g has none.
func (g *grip) close() error {
	if g == nil {
		return nil
	}
	var err error
	if g.file != nil {
		err = g.file.Close()
	}
	return errors.Join(err, g.dir.Close())
}

// gripTemp opens tmp to rename it as MoveFileEx does, with MOVEFILE_WRITE_THROUGH unless NoSync:
// the handle is opened with FILE_WRITE_THROUGH, so the rename does not return until it is on the
// disk, and a volume that refuses that gets a handle without it, and the write errNotThrough with
// the file in place. MoveFileEx takes paths, and would follow a folder swapped for a link out of
// root since the temp was made; here the folder is opened beneath root, and tmp is opened relative
// to its handle and renamed by its own, as os.Root renames, following no link at either name. The
// handle is compared with mine, the file written, so a file another account put at the temp name
// since the last check is refused as swapped, and nothing has moved. Under NoReplace the handle is
// what place links by (link, winapi.Link), opened without write-through as nothing is renamed by it:
// only the file written is linked, and NTFS refuses the link once a swap took its last name.
func gripTemp(root *os.Root, tmp string, _ *os.File, mine fs.FileInfo, o options) (*grip, error) {
	if o.noReplace {
		o.noSync, o.through = true, nil
	}
	d, err := root.Open(filepath.Dir(tmp)) // name's folder too
	if err != nil {
		return nil, err
	}
	g := &grip{dir: d}
	open := func(through bool, seam func(error) error) func() error {
		return func() (err error) {
			g.file, err = openTemp(d, tmp, through, o.noReplace, seam)
			return err
		}
	}
	err = winapi.Retried(open(!o.noSync, o.through))
	if errors.Is(err, errNotThrough) {
		g.late = err
		err = winapi.Retried(open(false, nil))
	}
	if err == nil {
		var got fs.FileInfo
		if got, err = g.file.Stat(); err == nil {
			if err = told(tmp, g.file, o); err == nil && !os.SameFile(mine, got) {
				err = swapped(tmp) // another file is at the temp name since the last check
			}
		}
	}
	if err != nil {
		return nil, errors.Join(err, g.close())
	}
	return g, nil
}

// openTemp opens tmp beneath the folder d to rename it, with FILE_WRITE_THROUGH when through, and with the right to link it when link, and
// sharing all, so a scanner's open does not fail on it. An open that refuses write-through is
// errNotThrough.
func openTemp(d *os.File, tmp string, through, link bool, seam func(open error) error) (*os.File, error) {
	opts := uint32(windows.FILE_OPEN_FOR_BACKUP_INTENT)
	if through {
		opts |= windows.FILE_WRITE_THROUGH
	}
	access := uint32(windows.DELETE | windows.FILE_READ_ATTRIBUTES)
	if link {
		access |= windows.FILE_WRITE_ATTRIBUTES // FileLinkInformation asks for it, as os.Root's link does
	}
	h, open := winapi.OpenAt(windows.Handle(d.Fd()), filepath.Base(tmp), winapi.How{Access: access, Share: windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE,
		Disposition: windows.FILE_OPEN, Options: opts, Attributes: windows.OBJ_CASE_INSENSITIVE})
	if through && seam != nil && testing.Testing() {
		open = seam(open) // a refused write-through, which no test machine's NTFS gives
	}
	if open != nil {
		var closed error
		if h != 0 { // the open was refused by the test, after it opened
			closed = windows.CloseHandle(h)
		}
		if through && (errors.Is(open, windows.STATUS_INVALID_PARAMETER) || errors.Is(open, windows.STATUS_NOT_SUPPORTED)) {
			return nil, errors.Join(fmt.Errorf("%w: %w", errNotThrough, open), closed)
		}
		return nil, errors.Join(winapi.Dos(open), closed)
	}
	return os.NewFile(uintptr(h), tmp), nil // owns h from here, and closes it
}

// sysOptions are options Windows alone reads.
type sysOptions struct {
	// set by the package's tests alone (seam_windows_test.go), read only under testing.Testing()
	through func(open error) error
	// noIndex makes indexed read the volume as one that gives a file no index, which no test
	// machine's volume does, so a test reaches the ErrNoFileIndex refusal
	noIndex bool
	// elsewhere takes this account as another in KeepOwner's look (winapi.Mine), so a test reaches
	// an old file another account owns, which a plain token cannot make
	elsewhere bool
	// linkAnswer answers in place of winapi.Link in link, so a test reaches a volume that keeps access
	// lists and no hard links (some SMB servers), which no test machine's NTFS is
	linkAnswer func() error
	// listless takes every volume as one that keeps no access lists (newFile), which no test
	// machine's NTFS is, so a test reaches the ErrNoAccessList refusal
	listless bool
}

func noLinks(err error) error {
	if errors.Is(err, windows.ERROR_INVALID_FUNCTION) || errors.Is(err, windows.ERROR_NOT_SUPPORTED) {
		return fmt.Errorf("%w: %w", ErrNoHardLinks, err)
	}
	return err
}

// syncFolder is nothing: a write's rename is written through instead (over); NoReplace's link and
// Keep's copy are kept by NTFS's log alone. SyncFolder, the caller's, flushes (flushOpen).
func syncFolder(*os.Root, string) error { return nil }

// indexed reports whether the file f is a handle to has a 64-bit file index that tells it apart from
// another, which os.SameFile needs. It reads the index and judges it through internal/fileid, the one
// helper write shares with the other packages that compare by file ID; o.noIndex is
// fileid's test seam, heeded only under testing.Testing(). FAT32 answers a non-zero directory-entry
// index (measured on one FAT32 volume) and is not refused; exFAT was not measured.
func indexed(f *os.File, o options) (bool, error) {
	return fileid.Indexed(f, o.noIndex)
}

// forceNoIndex is fileid's test seam as o carries it, for Append's hold (loghold.How).
func (s sysOptions) forceNoIndex() bool { return s.noIndex }

// toldAt opens name beneath root for its file index alone, with FILE_READ_ATTRIBUTES (the index
// needs no more) and following no link (OpenAt opens the reparse point itself, not its target on
// another volume), refuses a no-index volume by told, and checks with os.SameFile that the file it
// read the index of is fi, the one Swap looked at, so the index tested is the index of the file
// compared, not of one swapped in since the look. Sharing all, so a file another process holds is
// read; a file the account may move but not read is opened, where root.Open's read of the data
// would be refused. The handle is closed here.
func toldAt(root *os.Root, name string, fi fs.FileInfo, o options) error {
	d, err := root.Open(filepath.Dir(name))
	if err != nil {
		return err
	}
	h, err := winapi.OpenAt(windows.Handle(d.Fd()), filepath.Base(name), winapi.How{Access: windows.FILE_READ_ATTRIBUTES,
		Share: windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE, Disposition: windows.FILE_OPEN, Attributes: windows.OBJ_CASE_INSENSITIVE})
	if err != nil {
		return errors.Join(winapi.Dos(err), d.Close())
	}
	f := os.NewFile(uintptr(h), name)
	err = told(name, f, o)
	if err == nil {
		if got, serr := f.Stat(); serr != nil {
			err = serr
		} else if !os.SameFile(got, fi) {
			err = fmt.Errorf("%s changed between its look and the read of its file index, so it was not moved", name)
		}
	}
	return errors.Join(err, f.Close(), d.Close())
}

func aside(root *os.Root, from, to string) error {
	return winapi.Retried(func() error { return root.Rename(from, to) })
}

// pin opens the held log by its name beneath root, following no link at it, to read and sharing no
// delete: NTFS refuses the remove or rename of that name, and only that one, until it is let go.
// A name gone since the match, or not the held log, gives no unpin and no error.
func pin(root *os.Root, name string, mine fs.FileInfo) (unpin func() error, err error) {
	d, err := root.Open(filepath.Dir(name)) // a folder gone is an error, as the next try's create would be
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	h, err := winapi.OpenAt(windows.Handle(d.Fd()), filepath.Base(name), winapi.How{Access: windows.GENERIC_READ, Share: windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE, Disposition: windows.FILE_OPEN, Attributes: windows.OBJ_CASE_INSENSITIVE})
	if errors.Is(err, windows.STATUS_OBJECT_NAME_NOT_FOUND) || errors.Is(err, windows.STATUS_DELETE_PENDING) {
		return nil, nil // gone since the match
	} else if err != nil {
		return nil, err
	}
	p := os.NewFile(uintptr(h), name)
	got, err := p.Stat()
	if err != nil || !os.SameFile(got, mine) {
		return nil, errors.Join(err, p.Close()) // a link, a folder or another file is not the held log
	}
	return sync.OnceValue(p.Close), nil
}

// newFile makes name beneath root by an exclusive create with the owner-only access list
// (winapi.MakeFile), open as flag says: to write, or as os reopens a log, sharing
// read, write and delete, so the temp file can be renamed and a log moved while open. A volume
// that keeps no access lists is refused with ErrNoAccessList, and a last name ending in a dot or a
// space with winapi.ErrStrippedName, matching fs.ErrInvalid. o's listless, set by the package's tests alone and
// read only under testing.Testing() (in winapi), takes every volume as one, so a test reaches it.
// Under Shared the file is made with no list of its own (winapi.MakeFileShared), so it holds its
// folder's inheritable entries alone, and a volume that keeps no lists takes it.
func newFile(root *os.Root, name string, flag int, o options) (*os.File, error) {
	access := uint32(windows.FILE_GENERIC_WRITE)
	if flag&os.O_APPEND != 0 { // as os opens O_RDWR|O_APPEND, a log reopened: no FILE_WRITE_DATA, so each write goes to the end
		access = windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE&^windows.FILE_WRITE_DATA
	}
	share := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE)
	if o.shared {
		return winapi.MakeFileShared(root.Open, name, access, share)
	}
	return winapi.MakeFile(root.Open, name, access, share, winapi.Listless{Err: ErrNoAccessList, Test: o.listless})
}

// overOld clears Shared over a plain file KeepMode or KeepOwner keeps from: their rules here are
// unchanged by it, so the new file has the owner-only list, as without Shared, and the folder's list
// only where there is no old file.
func overOld(o options) options {
	o.shared = false
	return o
}

// move is Swap's move of from to to in root's folder, over nothing: from is opened beneath the
// folder's handle to delete, following no link, and renamed by its handle with no replace, which
// Windows allows of a program that runs. Unless NoSync the open asks for the rename written
// through to the disk; a volume that refuses that is renamed plainly and answers late. A sharing
// violation or a denial is tried again for about 600 ms (winapi.Retried), as a scanner holds a
// file that long.
func move(root *os.Root, from, to string, o options) (late bool, err error) {
	err = winapi.Retried(func() error {
		var once error
		late, once = moveOnce(root, from, to, !o.noSync)
		return once
	})
	return late, err
}

func moveOnce(root *os.Root, from, to string, through bool) (late bool, err error) {
	d, err := root.Open(".")
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	how := winapi.How{Access: windows.DELETE, Share: windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE,
		Disposition: windows.FILE_OPEN, Options: windows.FILE_OPEN_FOR_BACKUP_INTENT, Attributes: windows.OBJ_CASE_INSENSITIVE}
	if through {
		how.Options |= windows.FILE_WRITE_THROUGH
	}
	h, open := winapi.OpenAt(windows.Handle(d.Fd()), from, how)
	if through && (errors.Is(open, windows.STATUS_INVALID_PARAMETER) || errors.Is(open, windows.STATUS_NOT_SUPPORTED)) {
		late = true
		how.Options &^= windows.FILE_WRITE_THROUGH
		h, open = winapi.OpenAt(windows.Handle(d.Fd()), from, how)
	}
	if open != nil {
		return false, winapi.Dos(open)
	}
	defer func() { err = errors.Join(err, windows.CloseHandle(h)) }()
	return late, winapi.Rename(h, windows.Handle(d.Fd()), to, false)
}

// keeps is false: ready closes the file it holds, as an open handle stops a rename of its folder.
func (*grip) keeps() bool { return false }

// link hard-links the file g holds, the one gripTemp compared with the file written, at name, over
// nothing: by the handle (winapi.Link), so a file put at tmp since the last check is never linked,
// and none is taken away. The folder is opened beneath root again, as over does. Under
// testing.Testing() alone, s.linkAnswer answers in place of the link, which is then not made.
func (s sysOptions) link(root *os.Root, tmp, name string, g *grip) error {
	d, err := root.Open(filepath.Dir(name))
	if err != nil {
		return err
	}
	if s.linkAnswer != nil && testing.Testing() {
		err = s.linkAnswer()
	} else {
		err = winapi.Link(windows.Handle(g.file.Fd()), windows.Handle(d.Fd()), filepath.Base(name))
	}
	if err != nil {
		err = &os.LinkError{Op: "link", Old: tmp, New: name, Err: err}
	}
	return errors.Join(err, d.Close())
}

// rootOnPipes refuses root, before any open beneath it, where its folder is a host's pipes
// (winapi.RootOnPipes): each call of write that takes a root asks it first, and openFolder of each
// root it opens by a path.
func rootOnPipes(root *os.Root, name string) error { return winapi.RootOnPipes(root, name) }

// pipesPath reports whether p names a device or a pipe (fileid.DevicePath) or is on a host's
// pipes share (winapi.PipesShare), which openFolder refuses before any open.
func pipesPath(p string) bool { return fileid.DevicePath(p) || winapi.PipesShare(p) }

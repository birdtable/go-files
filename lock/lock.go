// Package lock keeps two runs of one program apart: one holder per lock
// file, held by the kernel, which drops it when the holder exits, so a lock
// cannot outlive its holder. The README says how, on each system.
//
// Built on the kernel's lock: flock(2) on Unix and LockFileEx on Windows, the calls
// github.com/gofrs/flock makes, on the file lock opened. It adds the lock file opened with
// no link followed, checked, locked, and its name read again once held, and a note of who holds it. TakeIn, WaitIn and HolderIn do the same below a held os.Root, by local name. The program configures the lock's path or its name below a root, how long Wait waits, by its context, and
// whether a note is written, at the path it names, and what it says.
// It imports the standard library, golang.org/x/sys and internal/fileid, and on Windows internal/winapi.
package lock

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Held is what a caller gets when somebody else has the lock. Not an error
// in the usual sense: the caller stands down quietly, or tries later.
//
// By and Since are what the holder's note says, and nothing proves them:
// whoever can make a name in the note's folder can write that file, and a
// killed holder's note names a process number the system may since have
// given to another. They are for a message only; never signal By or wait
// for it to exit.
type Held struct {
	By    int       // the process the holder's note names, 0 when the note is missing or torn
	Since time.Time // when the note says that process took the lock
}

func (h Held) Error() string {
	if h.By == 0 {
		return "another process holds the lock"
	}
	return fmt.Sprintf("process %d has held the lock since %s", h.By, h.Since.Format(time.RFC3339))
}

// IsHeld reports whether err is somebody else holding the lock, as opposed to
// the lock itself being broken. The two want different reactions.
func IsHeld(err error) bool { return errors.As(err, new(Held)) }

// A Lock is held until Release, or until the process that took it exits.
type Lock struct {
	f     *os.File // the lock file, opened and checked, and locked
	at    in       // where path and note are looked up: below the lock's own root (TakeIn), or by path
	path  string
	note  string // the caller's note path, "" for a lock with no note
	since time.Time
	kept  chan struct{} // closed by Release; till then a goroutine keeps the Lock from collection
}

// ErrNoNote is Note's refusal for a lock taken with no note path: it names no file to write.
var ErrNoNote = errors.New("the lock was taken with no note path, so it names no file for a note")

var errNoteIsLock = errors.New("is the lock file, so no note is written or removed there")

// Take locks path, or declines at once with Held, naming the holder when it
// wrote a note (Note) at note, and Held{} when it did not or note is "".
//
// note is the note's whole path, the caller's, as lock names none:
// the one string Note writes, Take, Wait and Release remove and Holder reads, so
// it may be wherever the note's readers look. "" is no note: Take removes none,
// declines with Held{}, and Note is refused with ErrNoNote. A note that cleans
// to path is refused before anything is opened. One the volume reads as the
// lock file (another case, a second name) is left alone by Take and Wait, which
// win the lock and remove nothing there, and refused by Note and Release, as a
// note written or removed there could take the lock file's name.
//
// Take writes no note of its own, so nothing learns of this process unless it
// calls Note, and it removes a note an earlier holder left at note, which
// names a process that holds nothing; a removal that fails leaves that note,
// for a message only, as Held says. A missing lock file is made with mode
// 0600, set on the descriptor, or on Windows with an access list that only
// this account and SYSTEM reach, none of its folder's inherited, where a
// volume that keeps no access lists (FAT, exFAT) is refused with
// ErrNoAccessList and nothing is made; one that is there keeps its mode and
// owner. Its folder must exist. A link at path, or what is not a plain file,
// is refused, never followed. A file this process may not open to read and
// write, or a missing one in a folder it may not make a file in, is refused,
// nothing made, matching fs.ErrPermission on every system, never Held. Release
// leaves the file behind: removing it races every process that has it open.
//
// A lock on a file another go-files call also holds, as write.Append's log, is
// not that call's hold on Windows, where Take locks the file's first byte and
// Append a byte past its end: there Take does not keep Append out, as it does on
// Unix, where both lock the whole file. Hold that call's own instead.
func Take(path, note string) (*Lock, error) { return acquire(in{}, path, note, nil, nil, false) }

// Wait locks path as Take does, with its note at note as Take's, but waits for
// the holder until ctx ends, and then fails with Held and the context's error,
// both found by errors.Is and errors.As. It always tries once. Any other
// failure is said as it is.
func Wait(ctx context.Context, path, note string) (*Lock, error) {
	return acquire(in{}, path, note, waiter(ctx), nil, false)
}

// TakeIn locks the file at name below root, as Take locks path, with its note at note below root,
// and every step by those local names through root, never by a path: the lock file is opened or
// made, checked, locked and its name read again, and the note written, read and removed, each
// beneath root, so a folder swapped above root, or a name that leaves it, a link among them, is
// never reached. A name or a note that leaves root is refused as os.Root refuses it, nothing made,
// and one whose last element is .. is refused before any open, wherever it leads.
// Within root, a link in a folder of name is followed as os.Root follows it, and one at name's
// last element is refused, as Take's is. The Lock keeps a root of its own, opened at root's
// folder, which Release closes: root may be closed once TakeIn returns. Holder of a note below a
// root is HolderIn.
func TakeIn(root *os.Root, name, note string) (*Lock, error) { return acquireIn(root, name, note, nil) }

// WaitIn locks name below root as TakeIn does, waiting for the holder as Wait does.
func WaitIn(ctx context.Context, root *os.Root, name, note string) (*Lock, error) {
	return acquireIn(root, name, note, waiter(ctx))
}

// HolderIn reads the note at note below root, as Holder reads one at a path, opened through root
// with no link followed at its last element.
func HolderIn(root *os.Root, note string) (Held, []byte, error) {
	if err := notUp(root, note); err != nil {
		return Held{}, nil, err
	}
	return holder(in{root}, note)
}

// acquireIn is acquire below a root of the lock's own, opened at root's folder.
func acquireIn(root *os.Root, name, note string, waited func() error) (*Lock, error) {
	for _, n := range []string{name, note} {
		if err := notUp(root, n); err != nil {
			return nil, err
		}
	}
	own, err := root.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	l, err := acquire(in{own}, name, note, waited, nil, false)
	if err != nil {
		return nil, errors.Join(err, own.Close())
	}
	return l, nil
}

// notUp refuses, before any open, a name whose last element is .., with root's own refusal of it
// where root gives one: open hands the last element to openat itself, so a last .. would reach the
// folder above root, where any other name that leaves root has its folder refused by root. A last ..
// is a folder, never a lock or a note, wherever it leads.
func notUp(root *os.Root, name string) error {
	if filepath.Base(name) != ".." {
		return nil
	}
	if _, err := root.Lstat(name); err != nil {
		return err
	}
	return &fs.PathError{Op: "openat", Path: name, Err: fs.ErrInvalid}
}

// waiter is Wait's wait between tries, until ctx ends.
func waiter(ctx context.Context) func() error {
	return func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Millisecond):
			return nil
		}
	}
}

// acquire takes the lock at path, looked up as at says, and tries again each time waited returns
// nil; Take has no waited and tries once. note is Take's note, or "". between runs after the lock file
// is opened and checked and before each lock, for the package's tests alone:
// it is read only under testing.Testing(), and Take and Wait pass nil. listless, set by a test
// alone and read only under testing.Testing() (in winapi), takes every volume as one with no
// access lists.
func acquire(at in, path, note string, waited func() error, between func(), listless bool) (*Lock, error) {
	if note != "" && filepath.Clean(note) == filepath.Clean(path) {
		return nil, fmt.Errorf("%s: the note's path %w", path, errNoteIsLock)
	}
	f, err := openLock(at, path, listless)
	if err != nil {
		return nil, err
	}
	for {
		if between != nil && testing.Testing() {
			between() // a name replaced after the check, which no test can time otherwise
		}
		got, err := tryLock(f)
		if err == nil && got {
			if err := atName(at, path, f); err != nil { // the name was replaced after the check
				return nil, errors.Join(err, unlock(f), f.Close())
			}
		}
		switch {
		case err != nil:
			return nil, errors.Join(fmt.Errorf("%s: %w", path, err), f.Close())
		case got:
			l := &Lock{f: f, at: at, path: path, note: note, since: time.Now(), kept: make(chan struct{})}
			go func() { <-l.kept; runtime.KeepAlive(l) }()
			_ = l.unnote() //nolint:errcheck // a stale note left stays for a message only, as Held says; the lock is this process's all the same
			return l, nil
		case waited == nil:
			return nil, errors.Join(held(at, note), f.Close())
		}
		if err := waited(); err != nil {
			return nil, errors.Join(fmt.Errorf("%w: %w", held(at, note), err), f.Close())
		}
	}
}

// beside says whether note's folder is the lock file's own, the one folder where a note takes the
// lock file's owner. A folder that cannot be looked at is not proven the same.
func beside(at in, path, note string) bool {
	a, err := at.stat(filepath.Dir(path))
	if err != nil {
		return false
	}
	b, err := at.stat(filepath.Dir(note))
	return err == nil && os.SameFile(a, b)
}

// held is what the note at note says, and Held{} for a lock with no note.
func held(at in, note string) Held {
	if note == "" {
		return Held{}
	}
	h, _, _ := holder(at, note) //nolint:errcheck // a note that cannot be read names nobody, and the lock is held all the same
	return h
}

// notLock refuses note where its name, not followed, is the lock file f has open: a spelling the
// volume alone reads as the lock's path, or a second name of the lock file. A name that cannot be
// looked at, "" among them, is no proof, and the write or removal there says its own error.
func notLock(at in, note string, f *os.File) error {
	li, err := at.lstat(note)
	if err != nil {
		return nil //nolint:nilerr // nothing at the name, or nothing seen there, is not the lock file
	}
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if err := indexAt(f, note); err != nil {
		return err
	}
	if os.SameFile(fi, li) {
		return fmt.Errorf("%s: %w", note, errNoteIsLock)
	}
	return nil
}

type note struct {
	PID   int             `json:"pid"`
	Since time.Time       `json:"since"`
	Note  json.RawMessage `json:"note,omitempty"`
}

// maxNote is the most of a note that Holder reads; more is torn.
const maxNote = 64 << 10

// Note writes who holds the lock, since when, and v as JSON, to the note path
// Take or Wait was given, by a temp file in the same folder and a rename, so a
// link at that name is replaced and its target is untouched. With note "" it
// is refused with ErrNoNote, and at the lock file as Take says. The file takes
// the lock file's mode, and on Unix its owner too only in the lock file's own
// folder: in any other it is this process's. Take and Wait
// write no note: a holder that wants one calls Note, and each call replaces
// the one before. A note of more than 64 KiB, the process and the time with
// it, is refused and the note before it stays. On Windows the rename is tried
// again for about 600 ms while another process has the note open, a reader or
// a scanner: the window is a fact of the system, not the caller's, and has no
// option.
func (l *Lock) Note(v any) error {
	if l.note == "" {
		return ErrNoNote
	}
	n := note{PID: os.Getpid(), Since: l.since}
	var err error
	if v != nil {
		if n.Note, err = json.Marshal(v); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(n)
	if err != nil {
		return err
	}
	if len(raw)+1 > maxNote {
		return fmt.Errorf("%s: the note is %d bytes with its newline, more than the %d Holder reads", l.note, len(raw)+1, maxNote)
	}
	if err := notLock(l.at, l.note, l.f); err != nil {
		return err
	}
	fi, err := l.f.Stat()
	if err != nil {
		return err
	}
	tmp, name, err := l.at.temp(l.note)
	if err != nil {
		return err
	}
	if err = owned(tmp, fi, beside(l.at, l.path, l.note)); err == nil {
		_, err = tmp.Write(append(raw, '\n'))
	}
	if err = errors.Join(err, tmp.Close()); err == nil {
		err = retried(func() error { return l.at.rename(name, l.note) }) // replaces a link there, never follows it
	}
	if err != nil {
		return errors.Join(err, l.at.remove(name))
	}
	return nil
}

// Holder reads the note at path, as Take's note, without taking the lock: who
// holds it, since when, and the raw JSON of the holder's Note, nil when none. A
// note that is missing, torn, a link, or not a plain file (a pipe, a socket)
// reads as Held{}, "another process holds the lock", never as free and
// never as an error. Holder does not say whether the lock is held; only Take
// does. What it reads is not proven to be the holder's, as Held says.
func Holder(path string) (Held, []byte, error) { return holder(in{}, path) }

// holder is Holder of the note at path, looked up as at says.
func holder(at in, path string) (Held, []byte, error) {
	f, err := openNote(at, path)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, errLink) || errors.Is(err, errNotPlain) {
		return Held{}, nil, nil
	}
	if err != nil {
		return Held{}, nil, err
	}
	defer f.Close() //nolint:errcheck // read only; a close of a read cannot lose what was read
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() { // a pipe or a device is never read: it can wait for as long as a writer holds it
		return Held{}, nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxNote+1))
	if err != nil {
		return Held{}, nil, err
	}
	var n note
	if len(raw) > maxNote || len(raw) == 0 || raw[len(raw)-1] != '\n' || json.Unmarshal(raw, &n) != nil || n.PID <= 0 {
		return Held{}, nil, nil
	}
	return Held{By: n.PID, Since: n.Since}, n.Note, nil
}

// File is refused with ErrNoInherit on every system: the lock is this
// process's, and Release drops it for every copy of the file, a child's too,
// so a child given the file would not keep it.
func (*Lock) File() (*os.File, error) { return nil, ErrNoInherit }

// Stat is the lock file's, read from the descriptor or handle Take opened
// and checked, which the lock was checked to be on: a caller that reached
// the lock file by another name compares the two with os.SameFile. After
// Release, or on nil, it is an error that is fs.ErrClosed.
func (l *Lock) Stat() (fs.FileInfo, error) {
	if l == nil || l.f == nil {
		return nil, fmt.Errorf("lock: %w", fs.ErrClosed)
	}
	return l.f.Stat()
}

// Release removes the note, if any, then drops the lock, for a fork still on
// its way to exec too, and closes TakeIn's root of the lock's own. Safe to call twice, and on nil. On Windows the removal
// is tried again for about 600 ms while another process holds the note open,
// a window of the system's, not the caller's; a note held past
// it stays, and is not Release's error: the lock is dropped all the same.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := errors.Join(l.unnote(), unlock(l.f), l.f.Close(), l.at.close())
	l.f = nil
	close(l.kept)
	return err
}

// unnote removes the note, a link there as the link, never its target, nor the lock file by
// another name. No note, none there, or one held open past the tries on Windows is no error.
func (l *Lock) unnote() error {
	if l.note == "" {
		return nil
	}
	if err := notLock(l.at, l.note, l.f); err != nil {
		return err
	}
	err := retried(func() error { // a removal does not follow a link
		if l.at.root != nil {
			return l.at.root.Remove(l.note)
		}
		return os.Remove(l.note) //nolint:gosec,nolintlint // the caller's note path; a removal does not follow a link; gosec's taint finding here comes and goes on windows
	})
	if errors.Is(err, fs.ErrNotExist) || inUse(err) {
		return nil
	}
	return err
}

func openLock(at in, path string, listless bool) (*os.File, error) {
	f, err := newLock(at, path, listless)
	made := err == nil
	if errors.Is(err, fs.ErrExist) {
		f, err = at.open(path, os.O_RDWR, 0)
	}
	if err != nil {
		return nil, openErr(err)
	}
	if made {
		err = f.Chmod(0o600) // whatever the umask
	}
	var fi os.FileInfo
	if err == nil {
		fi, err = f.Stat()
	}
	switch {
	case err != nil:
	case fi.Mode()&fs.ModeSymlink != 0: // on Windows the handle at a link is the link itself
		err = fmt.Errorf("%s: %w", path, errLink)
	case !fi.Mode().IsRegular():
		err = fmt.Errorf("%s: %w", path, errNotPlain)
	default:
		err = unlinked(at, path, f)
	}
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return f, nil
}

// atName refuses f unless the name at path, looked up as at says and not followed, is the file f has open.
func atName(at in, path string, f *os.File) error {
	li, err := at.lstat(path)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if err := indexAt(f, path); err != nil {
		return err
	}
	if !os.SameFile(fi, li) {
		return fmt.Errorf("%s: %w", path, errLink)
	}
	return nil
}

func openNote(at in, path string) (*os.File, error) {
	f, err := at.open(path, os.O_RDONLY|noBlock, 0) // no wait on a pipe
	if err != nil {
		return nil, openErr(err)
	}
	if err := unlinked(at, path, f); err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return f, nil
}

// ErrNoInherit is File's refusal, on every system: Release drops the lock
// for every copy of the lock file, so a child given it would not keep the
// lock. It matches errors.ErrUnsupported.
var ErrNoInherit = fmt.Errorf("the lock is this process's, and its Release drops it for every copy of the lock file, so a child given the file would not keep the lock, and File cannot give it: %w", errors.ErrUnsupported)

// ErrNoAccessList is Take's and Wait's refusal of a missing lock file Windows would not make
// this account's alone: on a volume that keeps no access lists (FAT, exFAT), one that will not
// say whether it does, a share that will not take this account as the owner, or a folder where
// this account cannot open by name what it makes there with that list, as under a restricted
// token or an AppContainer the list does not name, or on a share that knows this account by
// another SID, which a throwaway file made and removed first finds. The cause follows the text,
// and a denial matches fs.ErrPermission too. Nothing is made. It matches errors.ErrUnsupported.
var ErrNoAccessList = fmt.Errorf("the volume keeps no access lists, so a lock file made here would not be this account's alone, and nothing was made: %w", errors.ErrUnsupported)

var (
	errLink     = errors.New("is a link, or not the file at its name, so it is not followed")
	errNotPlain = errors.New("is not a plain file")
)

// in is where a lock's names are looked up: by local name below root, or by path where root is nil.
type in struct{ root *os.Root }

func (at in) lstat(name string) (fs.FileInfo, error) {
	if at.root == nil {
		return os.Lstat(name) //nolint:gosec // the caller's lock or note path; an Lstat follows no link
	}
	return at.root.Lstat(name)
}

func (at in) stat(name string) (fs.FileInfo, error) {
	if at.root == nil {
		return os.Stat(name)
	}
	return at.root.Stat(name)
}

func (at in) remove(name string) error {
	if at.root == nil {
		return os.Remove(name)
	}
	return at.root.Remove(name)
}

func (at in) rename(from, to string) error {
	if at.root == nil {
		return os.Rename(from, to) //nolint:gosec // to the caller's note path; the rename replaces a link there, never follows it
	}
	return at.root.Rename(from, to)
}

// temp makes Note's temp file beside note by an exclusive create, 0600 before owned sets its mode,
// and is it and its name as at looks it up.
func (at in) temp(note string) (*os.File, string, error) {
	if at.root == nil {
		f, err := os.CreateTemp(filepath.Dir(note), filepath.Base(note)+".*")
		if err != nil {
			return nil, "", err
		}
		return f, f.Name(), nil
	}
	name := note + "." + rand.Text()
	f, err := at.root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600) // an exclusive create follows no link
	return f, name, err
}

func (at in) close() error {
	if at.root == nil {
		return nil
	}
	return at.root.Close()
}

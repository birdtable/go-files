package write

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/birdtable/go-files/internal/loghold"
)

// LogOption is one way a log Append keeps differs from the plain one, which is made 0600, and
// Aside, which names where a full log moves, as Append takes no default. It is a type of its own,
// so an Option Append does not read, as Owner or Check, is no option it takes.
type LogOption func(*options)

// LogMode is the mode of a log Append makes, at first and after each move, exactly: the umask
// does not narrow it. A log already there keeps its own. It must give the owner read and write,
// as Append opens the log for both. Windows refuses it with ErrNoMode, as Mode: a new log there
// has the owner-only access list Option says.
func LogMode(perm fs.FileMode) LogOption {
	return func(o *options) { o.perm, o.modeSet = perm&fs.ModePerm, true }
}

// Aside is the name beneath root a full log is moved to, over the one there. Append takes no
// default name: without Aside, or with Aside(""), it is refused with ErrNoAside before anything is
// opened, and no line is written, so where an old log goes is always the caller's.
// A caller that keeps more than one old log moves the last one on itself before it calls Append.
// It must not be the log's own name: as written or cleaned it is refused before anything is
// opened, and in a spelling the volume alone reads as the log (another case where the volume
// ignores case, or on Windows a trailing dot or space) it is refused at the move, which finds the
// file at it is the log, and no line is written.
func Aside(name string) LogOption { return func(o *options) { o.aside = name } }

// ErrNoAside is Append's refusal when no Aside names where a full log moves.
var ErrNoAside = errors.New("no Aside names where a full log moves, and Append takes no default, so no line is written")

// Append adds line and a newline to the log at name beneath root, in one write, and keeps the log
// under limit bytes: a line that would take it past limit first moves the log aside, to Aside's
// name, over the one there, and goes into a new log. A line is never cut, so a line longer than
// limit is a log of its own. A log that is not there is made 0600, or LogMode's mode.
//
// A log whose last byte is not a newline has a torn last line, which a crash in another writer's
// write can leave: Append ends it, with a newline put before line in the same one write, so the
// new line never joins the torn one. The torn line is kept as it is, never cut, and a reader sees
// it as a line of its own. The newline counts toward limit; a log moved aside keeps its torn line.
//
// A link at the name or at the name aside, anything there that is not a plain file, and a log with
// a second name are refused, and the line goes nowhere. Two processes appending at once each hold
// the log for the look, the move and the write, so no line is cut. The hold is Append's own, which
// Append alone takes: one another process's Append keeps is waited on until ctx ends, and then
// refused with ctx's error, found by errors.Is, and the line goes nowhere. Append always tries the
// hold once, so a ctx already ended still writes to a log no other process holds. lock.Take or
// lock.Wait on the log keeps Append out on Unix, where both lock the whole file, and not on
// Windows, where Append locks a byte past the log's end and lock its first byte. On Windows a name whose last part ends in a dot or
// a space is refused before anything is opened, with an error matching fs.ErrInvalid, as the
// system strips the end and would reach another log. On Windows a move that another process holds back
// is tried again for about 600 ms, as File's rename is: the window is a fact of the system, not
// the caller's. Any other failure is said as it is.
func Append(ctx context.Context, root *os.Root, name string, line []byte, limit int64, opts ...LogOption) error {
	o := settle(nil)
	for _, opt := range opts {
		opt(&o)
	}
	return appendWith(ctx, root, name, line, limit, o)
}

// appendWith is Append with a test's options: steps "look", "held" and "checked" (once pinned).
// Its options' perm is the mode of a log it makes, and aside the name a full log moves to, which
// "" leaves unnamed and refused.
func appendWith(ctx context.Context, root *os.Root, name string, line []byte, limit int64, o options) error {
	if err := rootOnPipes(root, name); err != nil {
		return fmt.Errorf("%s beneath %s: %w", name, root.Name(), err)
	}
	if err := logNamed(name); err != nil {
		return fmt.Errorf("%s beneath %s: %w", name, root.Name(), err)
	}
	if err := logOptions(name, o); err != nil {
		return fmt.Errorf("%s beneath %s: %w", name, root.Name(), err)
	}
	line = append(slices.Clip(line), '\n')
	for range tries {
		again, err := appendOnce(ctx, root, name, line, limit, o)
		if err != nil {
			return fmt.Errorf("%s beneath %s: %w", name, root.Name(), err)
		}
		if !again {
			return nil
		}
	}
	return fmt.Errorf("%s beneath %s was moved by another writer %d times while this one tried", name, root.Name(), tries)
}

// appendOnce writes line, or moves the log aside, and answers whether to go again.
func appendOnce(ctx context.Context, root *os.Root, name string, line []byte, limit int64, o options) (again bool, err error) {
	f, mine, at, err := loghold.Take(ctx, root, name, logHow(root, name, o))
	if errors.Is(err, loghold.ErrMoved) {
		return true, nil // moved by another writer before this one held it
	}
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, loghold.Release(f)) }()
	unpin, err := pin(root, name, mine)
	if unpin == nil || err != nil {
		return err == nil, err // no unpin and no error: moved since the match, or not the held log
	}
	defer func() { err = errors.Join(err, unpin()) }()
	o.reached("checked")
	if err := loghold.Opened(f, name, at); err != nil { // counted by the look that matched: a link the open followed, swapped for a hard link
		return false, err
	}
	torn, err := tornAt(f, mine.Size())
	if err != nil {
		return false, err
	}
	if mine.Size() > 0 && mine.Size()+ends(torn)+int64(len(line)) > limit {
		if fi, err := root.Lstat(o.aside); err == nil && os.SameFile(fi, at) {
			// a spelling logOptions cannot see: another case on a volume that ignores it, or on
			// Windows a trailing dot or space, which os.Root strips
			return false, fmt.Errorf("%s is the log itself, by another spelling, and the log is not moved over it, so no line is written", o.aside)
		} else if err == nil && !fi.Mode().IsRegular() {
			return false, fmt.Errorf("%s is %v, not a plain file, and the log is not moved over it", o.aside, fi.Mode().Type())
		}
		return true, errors.Join(unpin(), aside(root, name, o.aside)) // the pin is let go first: the rename needs delete
	}
	if torn {
		line = append([]byte{'\n'}, line...) // one write: the torn line ended and the new one after it
	}
	_, err = f.Write(line)
	return false, err
}

// tornAt reports whether the log open at f, size bytes long, ends in anything but a newline: a last
// line a crash cut off. It reads the last byte through f, under Append's hold.
func tornAt(f *os.File, size int64) (bool, error) {
	if size == 0 {
		return false, nil
	}
	b := make([]byte, 1)
	if _, err := f.ReadAt(b, size-1); err != nil {
		return false, fmt.Errorf("the log's last byte could not be read, so no line is written: %w", err)
	}
	return b[0] != '\n', nil
}

// ends is the newline a torn log's next line takes before it.
func ends(torn bool) int64 {
	if torn {
		return 1
	}
	return 0
}

// logOptions refuses what Append's options cannot do, before anything is opened: no name aside,
// Shared, which is File's, a mode where a file has none, a mode its owner cannot read and write the log by, and a log moved
// aside over itself by a name that cleans to its own. A spelling the volume alone reads as the
// log is appendOnce's to refuse, at the move.
func logOptions(name string, o options) error {
	if o.aside == "" {
		return ErrNoAside
	}
	if o.shared { // File's alone, reached as LogOption(Shared()): a log is its owner's
		return fmt.Errorf("a log takes LogMode and not Shared, which is File's, so no line is written: %w", fs.ErrInvalid)
	}
	if o.modeSet {
		if err := modeGiven(); err != nil {
			return err
		}
	}
	if o.perm&0o600 != 0o600 {
		return fmt.Errorf("a log's mode gives its owner read and write, as Append opens it for both, and %#o does not, so no line is written", o.perm)
	}
	if filepath.Clean(o.aside) == filepath.Clean(name) {
		return fmt.Errorf("the log would be moved aside to its own name, %s, so no line is written", o.aside)
	}
	return nil
}

// logHow is what Append brings to loghold.Take for the log at name beneath root: its create, which
// makes the log by an exclusive create and gives it o's perm on the descriptor, whatever the umask;
// fileid's test seam; and its test steps "look" and "held".
func logHow(root *os.Root, name string, o options) loghold.How {
	return loghold.How{
		Create: func(flag int) (*os.File, error) {
			f, err := newFile(root, name, flag, o)
			if err != nil {
				return nil, err
			}
			if err := mode(f, o.perm); err != nil {
				return nil, errors.Join(err, f.Close())
			}
			return f, nil
		},
		ForceNoIndex: o.forceNoIndex(),
		Look:         func() { o.reached("look") },
		Held:         func() { o.reached("held") },
	}
}

package write

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// ErrNotSwapped is Swap's error when the new file could not take the old one's name and the old
// one could not be put back either: the old file is at aside, and nothing, or what took the name
// meanwhile, is at file. The caller moves aside back by hand. It is also the error when a file at
// fresh that is not the one Swap looked at took file's name, and the undo of that move failed: file
// may then hold that file, which Swap never looked at and the caller must not run.
var ErrNotSwapped = errors.New("the new file did not take the name, and the old file could not be put back, so it is left aside")

// Swap puts fresh in file's place where file may be a program that is running, and keeps the old
// file at aside. The old file is moved aside first, then fresh takes its name, and if that second
// move fails the old file is moved back, so file is never left without one of the two: Windows
// will not write over or remove a program that runs, but it will rename one. file, fresh and aside
// are three names in one folder, and both moves stay in it, so fresh is a file the caller wrote
// beside the program, by File with Runnable. file and fresh must be plain files, and neither is
// followed if it is a link. Nothing is ever written over: a file at aside is refused with an error
// matching fs.ErrExist and nothing moves, and the caller names another, or removes the one there
// after it has checked it; a file put at file's name between the two moves is kept, and refused as
// the second move's error. A file moved aside or moved in that is not the one looked at is
// refused, and each move undone: the file at aside goes back to file, and one moved in goes back
// to fresh before it, or the error is ErrNotSwapped. IfHolds refuses with ErrChanged a file that holds anything else, before
// anything moves. NoSync flushes no folder; without it the folder is flushed once, after the
// swap, and an error from that flush is ErrNotDurable, with the new file in place. Every other
// Option, Mode and Shared among them, is refused, with an error matching fs.ErrInvalid. A second move that fails, and whose
// put-back moved the old file back, is that move's error; one whose put-back failed too is
// ErrNotSwapped, with both causes.
//
// Two names that differ only in case are refused as one, as Windows and macOS take them.
//
// On Unix each move is a hard link and the removal of the first name, so a crash between the two
// leaves both names, never neither; a link the volume refuses moves nothing. On Windows each move is a rename that goes over
// nothing, by the file's own handle opened beneath the folder, written through to the disk unless
// NoSync, and tried again for about 600 ms while a scanner holds the file, as File's rename is.
func Swap(file, fresh, aside string, opts ...Option) error {
	if err := threeNames(file, fresh, aside); err != nil {
		return err
	}
	o := settle(opts)
	if err := swapTakes(o); err != nil {
		return fmt.Errorf("a swap of %s: %w", file, err)
	}
	root, err := os.OpenRoot(filepath.Dir(file))
	if err != nil {
		return err
	}
	err = swapIn(root, filepath.Base(file), filepath.Base(fresh), filepath.Base(aside), o)
	if err := errors.Join(err, root.Close()); err != nil {
		return fmt.Errorf("a swap of %s for %s, the old one to %s: %w", file, fresh, aside, err)
	}
	return nil
}

// threeNames refuses three names that are not three plain names in one folder: two whose
// confine.Portable keys are equal are one name to a folder that folds case or normal form, as
// APP and app, café composed and decomposed, or paß and pass are on some system.
func threeNames(file, fresh, aside string) error {
	dir := filepath.Dir(file)
	keys := make([]string, 0, 3)
	for _, p := range []string{file, fresh, aside} {
		if filepath.Dir(p) != dir {
			return fmt.Errorf("a swap of %s: %s is not in %s, and the three files must share one folder: %w", file, p, dir, fs.ErrInvalid)
		}
		key, err := confine.Portable(filepath.Base(p))
		if err != nil {
			return fmt.Errorf("a swap of %s: %q is not one plain name in its folder (%w): %w", file, filepath.Base(p), err, fs.ErrInvalid)
		}
		keys = append(keys, key)
	}
	if keys[0] == keys[1] || keys[0] == keys[2] || keys[1] == keys[2] {
		return fmt.Errorf("a swap of %s: %s, %s and %s are not three names: %w", file, filepath.Base(file), filepath.Base(fresh), filepath.Base(aside), fs.ErrInvalid)
	}
	return nil
}

// swapTakes refuses every Option but IfHolds and NoSync, and the test's seams.
func swapTakes(o options) error {
	if o.modeSet || o.shared || o.keepMode || o.keepOwner || o.uid != -1 || o.gid != -1 || o.noReplace || o.gone || o.runnable || o.check != nil {
		return fmt.Errorf("Swap takes IfHolds and NoSync alone: it moves two files and writes none: %w", fs.ErrInvalid)
	}
	return nil
}

// plain looks at name beneath root, following no link, and refuses it unless it is a regular file,
// the only kind Swap moves; noun ends the refusal ("one is swapped", "one is swapped in").
func plain(root *os.Root, name, noun string) (os.FileInfo, error) {
	fi, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is %v, not a plain file, and only %s", name, fi.Mode().Type(), noun)
	}
	return fi, nil
}

func swapIn(root *os.Root, file, fresh, aside string, o options) error {
	old, err := plain(root, file, "one is swapped")
	if err != nil {
		return err
	}
	if o.holds != nil {
		if err := still(root, file, *o.holds, o); err != nil {
			return err
		}
	}
	nu, err := plain(root, fresh, "one is swapped in")
	if err != nil {
		return err
	}
	// A no-index file, or one swapped in at either name since the look above, is refused before Swap
	// moves anything: bothTold reads each name's index and holds it to the look by os.SameFile.
	o.reached("told")
	if err := bothTold(root, file, old, fresh, nu, o); err != nil {
		return err
	}
	o.reached("swap")
	late, err := move(root, file, aside, o)
	if err != nil {
		return err
	}
	if at, err := root.Lstat(aside); err != nil || !os.SameFile(at, old) {
		return errors.Join(fmt.Errorf("%s was swapped for another file before it was moved aside to %s", file, aside), err, back(root, aside, file, o))
	}
	o.reached("aside")
	more, err := move(root, fresh, file, o)
	if err != nil {
		if perr := back(root, aside, file, o); perr != nil {
			return fmt.Errorf("%w: %w: %w", ErrNotSwapped, err, perr)
		}
		return err
	}
	if err := movedIn(root, file, fresh, aside, nu, o); err != nil {
		return err
	}
	if late || more {
		return fmt.Errorf("%s %w: %w", file, ErrNotDurable, errNotThrough)
	}
	if o.noSync {
		return nil
	}
	flush := syncFolder
	if o.flush != nil && testing.Testing() {
		flush = o.flush
	}
	if err := flush(root, "."); err != nil {
		return fmt.Errorf("%s %w: %w", file, ErrNotDurable, err)
	}
	return nil
}

// movedIn refuses a file at file that is not nu, the one swapIn looked at before it moved fresh
// in, and undoes both moves: the file at file goes back to fresh, then the old file at aside back to
// file. An undo that fails is ErrNotSwapped.
func movedIn(root *os.Root, file, fresh, aside string, nu os.FileInfo, o options) error {
	at, err := root.Lstat(file)
	if err == nil && os.SameFile(at, nu) {
		return nil
	}
	bad := errors.Join(fmt.Errorf("%s was swapped for another file before it was moved in to %s", fresh, file), err)
	_, uerr := move(root, file, fresh, o)
	if uerr == nil {
		uerr = back(root, aside, file, o)
	}
	if uerr != nil {
		return fmt.Errorf("%w: %w: %w", ErrNotSwapped, bad, uerr)
	}
	return bad
}

// back moves the old file at aside back to file, over nothing.
func back(root *os.Root, aside, file string, o options) error {
	if _, err := move(root, aside, file, o); err != nil {
		return fmt.Errorf("the put-back of %s to %s: %w", aside, file, err)
	}
	return nil
}

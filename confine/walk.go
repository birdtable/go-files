package confine

// Open by the walk, where no kernel call opens with no link followed. The README says each check.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// The steps where another process could change the path, for a test's reached.
const (
	stepWalk      = "between two steps of the walk"
	stepOpen      = "before the open"
	stepWalkAgain = "before the second walk"
	stepMade      = "before the walk of the made file"
)

// stop is where a walk stopped: the last folder, open, for the caller to close, nil when a
// component before the last is missing; the last component's name; and what it is, nil when
// missing.
type stop struct {
	dir  *os.Root
	name string
	info fs.FileInfo
}

// walkTo walks name, clean and local, by handle: each component is looked at in the folder opened
// before it, and each folder opened is refused unless it is the one looked at.
func walkTo(root *os.Root, name string, reached func(step string)) (stop, error) {
	parts := strings.Split(name, string(filepath.Separator))
	cur, err := root.OpenRoot(".")
	if err != nil {
		return stop{}, err
	}
	for i, part := range parts {
		at := filepath.ToSlash(filepath.Join(parts[:i+1]...))
		last := i == len(parts)-1
		look, err := lookAt(cur, part, at, last)
		switch {
		case err != nil:
			return stop{}, errors.Join(err, cur.Close())
		case last:
			return stop{dir: cur, name: part, info: look}, nil
		case look == nil:
			return stop{}, cur.Close()
		}
		reached(stepWalk)
		sub, err := next(cur, part, look, at)
		if err = errors.Join(err, cur.Close()); err != nil {
			if sub != nil {
				err = errors.Join(err, sub.Close())
			}
			return stop{}, err
		}
		cur = sub
	}
	return stop{}, errors.Join(&fs.PathError{Op: opWalk, Path: filepath.ToSlash(name), Err: errors.New("it names nothing")}, cur.Close())
}

// lookAt refuses a link or reparse point at part, and before the last component anything not a
// folder. A missing part answers nil and no error.
func lookAt(cur *os.Root, part, at string, last bool) (fs.FileInfo, error) {
	look, err := cur.Lstat(part)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if what := linkKind(look.Mode()); what != "" {
		return nil, &fs.PathError{Op: opWalk, Path: at, Err: fmt.Errorf("it is %s, and no link is followed: %w", what, ErrOutside)}
	}
	if !last && !look.IsDir() {
		return nil, &fs.PathError{Op: opWalk, Path: at, Err: errors.New("it is not a folder")}
	}
	return look, nil
}

// next opens the folder part in cur and refuses it unless it is look: os.Root follows a relative
// link that stays inside. Gone, no folder, outside, or ELOOP (a link gone before os.Root read it)
// where a look found a folder, is a component that changed.
func next(cur *os.Root, part string, look fs.FileInfo, at string) (*os.Root, error) {
	changed := changedAt(at)
	sub, err := cur.OpenRoot(part)
	if swappedAt(cur, err) {
		return nil, changed
	}
	if err != nil {
		return nil, err
	}
	got, err := sub.Stat(".")
	if err != nil {
		return nil, errors.Join(err, sub.Close())
	}
	if !os.SameFile(look, got) {
		return nil, errors.Join(changed, sub.Close())
	}
	if err := indexRoot(sub, got, at, changed); err != nil {
		return nil, errors.Join(err, sub.Close())
	}
	return sub, nil
}

// changedAt is the refusal of the component at, which changed while it was walked.
func changedAt(at string) error {
	return &fs.PathError{Op: opWalk, Path: at, Err: fmt.Errorf("a component changed while it was walked: %w", ErrOutside)}
}

// swappedAt is whether err, from the open of a component in cur, is what a swap there answers.
func swappedAt(cur *os.Root, err error) bool {
	return escapes(cur, err) || errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || loops(err)
}

// walkOpen is Open by the walk. A file the walk found is opened in the folder it holds and refused
// unless it is that file. A missing one is made by an exclusive create there and refused unless a
// second walk finds it; when another process made it first, and the caller did not ask for
// os.O_EXCL, it is walked again and that one opened.
func walkOpen(root *os.Root, name string, flag int, perm fs.FileMode, reached func(step string)) (*os.File, error) {
	w, err := walkTo(root, name, reached)
	if err != nil {
		return nil, err
	}
	if w.dir == nil {
		return nil, &fs.PathError{Op: opOpen, Path: filepath.ToSlash(name), Err: fs.ErrNotExist}
	}
	if w.info == nil {
		f, err := create(root, w, name, flag&^os.O_TRUNC, perm, reached)
		if f, err = done(w.dir, f, err); err != nil || f != nil {
			return f, err
		}
		reached(stepWalkAgain)
		if w, err = walkTo(root, name, reached); err != nil {
			return nil, err
		}
		if w.info == nil {
			gone := &fs.PathError{Op: opOpen, Path: filepath.ToSlash(name),
				Err: fmt.Errorf("it was made and taken away while it was opened, and nothing was done to it: %w", ErrOutside)}
			if w.dir == nil {
				return nil, gone
			}
			return done(w.dir, nil, gone)
		}
	}
	f, err := openWalked(w, name, flag, reached)
	return done(w.dir, f, err)
}

// done closes dir and answers f and err; when the close fails it answers nil and both errors,
// with f closed.
func done(dir *os.Root, f *os.File, err error) (*os.File, error) {
	if closed := dir.Close(); closed != nil {
		if f != nil {
			closed = errors.Join(closed, f.Close())
		}
		return nil, errors.Join(err, closed)
	}
	return f, err
}

// moved is the refusal of a file that changed between the walk and the open. got is what the open
// answered, or nil when it answered nothing; when got is not a regular file the refusal matches
// ErrNotRegular as well, so ReadCapped refuses a name swapped for a folder or a pipe by ErrNotRegular
// whether a walk or the system's own open saw the swap.
func moved(name string, got fs.FileInfo) error {
	err := fmt.Errorf("it changed between the walk and the open, and nothing was done to it: %w", ErrOutside)
	if got != nil && !got.Mode().IsRegular() {
		err = fmt.Errorf("%w; it opened as %s: %w", err, got.Mode().Type(), ErrNotRegular)
	}
	return &fs.PathError{Op: opOpen, Path: filepath.ToSlash(name), Err: err}
}

// openWalked opens w.name in w.dir and refuses it unless it is w.info. os.O_TRUNC empties the file
// only after every check.
func openWalked(w stop, name string, flag int, reached func(step string)) (*os.File, error) {
	trunc := flag&os.O_TRUNC != 0
	flag &^= os.O_TRUNC
	if flag&(os.O_CREATE|os.O_EXCL) == os.O_CREATE|os.O_EXCL {
		return nil, &fs.PathError{Op: opOpen, Path: filepath.ToSlash(name), Err: fs.ErrExist}
	}
	reached(stepOpen)
	f, err := w.dir.OpenFile(w.name, flag&^os.O_CREATE, 0)
	if escapes(w.dir, err) || loops(err) {
		return nil, moved(name, nil)
	}
	if err != nil {
		return nil, err
	}
	if err := checkOpened(f, name, w.info, flag); err != nil {
		return nil, errors.Join(err, f.Close())
	}
	if trunc {
		if err := f.Truncate(0); err != nil {
			return nil, errors.Join(err, f.Close())
		}
	}
	return f, nil
}

// create makes w.name in w.dir by an exclusive create. When another process made it first, and
// flag does not ask for os.O_EXCL, it answers nil and no error. Without os.O_CREATE it makes
// nothing.
func create(root *os.Root, w stop, name string, flag int, perm fs.FileMode, reached func(step string)) (*os.File, error) {
	if flag&os.O_CREATE == 0 {
		return nil, &fs.PathError{Op: opOpen, Path: filepath.ToSlash(name), Err: fs.ErrNotExist}
	}
	reached(stepOpen)
	f, err := w.dir.OpenFile(w.name, flag|os.O_EXCL, perm)
	if err == nil {
		if err := checkMade(root, f, name, reached); err != nil {
			return nil, errors.Join(err, f.Close())
		}
		return f, nil
	}
	if !errors.Is(err, fs.ErrExist) || flag&os.O_EXCL != 0 {
		return nil, err
	}
	return nil, nil
}

// checkOpened refuses f unless it is walked, of walked's type, and, opened to be written, has one
// name. The type is asked as well because os.SameFile asks only the device and the file number, and
// a system that gives a removed file's number to the next file made, as Linux does, has a folder
// made at the name in its place pass as the file walked.
func checkOpened(f *os.File, name string, walked fs.FileInfo, flag int) error {
	got, err := f.Stat()
	if err != nil {
		return err
	}
	if err := indexHeld(f, name); err != nil {
		return err
	}
	if !os.SameFile(got, walked) || got.Mode().Type() != walked.Mode().Type() {
		return moved(name, got)
	}
	return namesOf(f, name, flag)
}

// checkMade refuses f, just made at name, unless a second walk finds f there with no link: a
// folder swapped for a link since the walk, and swapped back, leaves f elsewhere.
func checkMade(root *os.Root, f *os.File, name string, reached func(step string)) error {
	reached(stepMade)
	again, err := walkTo(root, name, reached)
	if err != nil {
		return err
	}
	if again.dir != nil {
		if err := again.dir.Close(); err != nil {
			return err
		}
	}
	got, err := f.Stat()
	if err != nil {
		return err
	}
	if err := indexHeld(f, name); err != nil {
		return err
	}
	if again.info == nil || !os.SameFile(got, again.info) {
		return &fs.PathError{Op: opOpen, Path: filepath.ToSlash(name),
			Err: fmt.Errorf("it was made somewhere else than the walk found, and nothing was written to it: %w", ErrOutside)}
	}
	return nil
}

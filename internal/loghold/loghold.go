// Package loghold is the one hold write.Append keeps on its log, and the open, refusals and check
// that come before it: write's Append takes it for each line, and filestest.HoldLog takes it for a
// test, so a test's hold is the hold, not a copy of its rules. The hold is flock(2) on the whole
// file on Unix, and on Windows a LockFileEx lock of one byte past the end of any log, so a reader is
// never refused a range. Its two system files hold the kernel's lock calls, as lock's two do: the
// register path-lock row refuses them in every other file.
// It imports the standard library, golang.org/x/sys and confine on Windows, and internal/fileid.
package loghold

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/birdtable/go-files/internal/fileid"
)

// ErrMoved is another writer's move, removal or making of the log between the look and the open,
// or before the hold: the caller tries again.
var ErrMoved = errors.New("the log was moved between its look and its hold")

// How is what one caller of Take brings.
type How struct {
	// Create makes the log by an exclusive create, open as flag says, when the look finds none, so
	// it follows no link put there since. Its mode, and on Windows its access list, are the caller's.
	Create func(flag int) (*os.File, error)
	// ForceNoIndex is internal/fileid's test seam, heeded only under testing.Testing() (in fileid).
	ForceNoIndex bool
	// Look and Held are a test's steps, after the look and once the log is held; nil is none.
	Look, Held func()
}

// Take opens the log at name beneath root and holds it, waiting for another process to let go of
// it until ctx ends. The log is opened to read and to append, a plain file with one name by the
// look and by its descriptor, or made by how.Create. Once held, a volume that gives a file no
// index is refused with fileid.ErrNoFileIndex, and the file at name must still be the one held,
// or Take lets go and answers ErrMoved. It answers the held file, what it reads as, and the look
// at name that matched it; Release lets go of it.
func Take(ctx context.Context, root *os.Root, name string, how How) (f *os.File, mine, at fs.FileInfo, err error) {
	f, err = Open(root, name, how)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := hold(ctx, f, name); err != nil {
		return nil, nil, nil, errors.Join(err, f.Close())
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, Release(f))
			f = nil
		}
	}()
	if how.Held != nil {
		how.Held()
	}
	mine, err = f.Stat()
	if err != nil {
		return f, nil, nil, err
	}
	switch ok, err := fileid.Indexed(f, how.ForceNoIndex); {
	case err != nil:
		return f, nil, nil, err
	case !ok: // a volume that gives no file index cannot tell the held log from another
		return f, nil, nil, fmt.Errorf("%s: %w", name, fileid.ErrNoFileIndex)
	}
	at, err = root.Lstat(name)
	if err != nil || !os.SameFile(mine, at) {
		return f, nil, nil, ErrMoved // moved by another writer before this one held it
	}
	return f, mine, at, nil
}

// Release lets go of the hold Take took on f, and then closes f, so a fork on its way to exec that
// shares f does not keep the hold.
func Release(f *os.File) error { return errors.Join(letGo(f), f.Close()) }

// Open opens the log at name beneath root to read and to append, a plain file by the look and by
// Opened on the descriptor, or makes it by how.Create, and answers ErrMoved where another writer
// moved, removed or made it between the look and the open. It holds nothing: Take holds.
func Open(root *os.Root, name string, how How) (*os.File, error) {
	fi, err := root.Lstat(name)
	flag := os.O_RDWR | os.O_APPEND | noWait // read too: LockFileEx takes no handle that may only append
	switch {
	case errors.Is(err, fs.ErrNotExist):
		fi = nil
	case err != nil:
		return nil, err
	case !fi.Mode().IsRegular():
		return nil, fmt.Errorf("%s is %v, not a plain file, and no line is written to it", name, fi.Mode().Type())
	}
	if how.Look != nil {
		how.Look()
	}
	var f *os.File
	if fi == nil {
		f, err = how.Create(flag)
	} else {
		f, err = root.OpenFile(name, flag, 0)
	}
	switch {
	case fi == nil && errors.Is(err, fs.ErrExist), fi != nil && errors.Is(err, fs.ErrNotExist):
		return nil, ErrMoved
	case err != nil:
		return nil, err
	case fi == nil:
		return f, nil
	}
	got, err := f.Stat()
	if err == nil {
		err = Opened(f, name, got)
	}
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return f, nil
}

// Opened refuses a log that is not plain, or has a second name, which can be a file outside root.
// A file put at the name since the look is Take's to find, by its check at the hold.
func Opened(f *os.File, name string, got fs.FileInfo) error {
	if !got.Mode().IsRegular() {
		return fmt.Errorf("%s changed between its look and its open to %v, not a plain file, and no line was written to it", name, got.Mode().Type())
	}
	n, err := names(f, got)
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("%s has %d names, and a second name can be a file outside the root, so no line is written to it", name, n)
	}
	return nil
}

// hold takes the log, waiting for another process to let go of it until ctx ends. It always tries
// once, and a ctx that ends while it waits is answered at once, not at the next try.
func hold(ctx context.Context, f *os.File, name string) error {
	for wait := time.Millisecond; ; wait = min(2*wait, 20*time.Millisecond) {
		got, err := tryHold(f)
		if err != nil || got {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s is held by another process, and no line was written to it: %w", name, ctx.Err())
		case <-time.After(wait):
		}
	}
}

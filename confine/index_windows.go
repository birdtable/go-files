package confine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"

	"github.com/birdtable/go-files/internal/fileid"
	"github.com/birdtable/go-files/internal/winapi"
)

// indexHeld refuses the file f, named name, on a Windows volume that gives it no 64-bit index
// os.SameFile can tell it apart from another put in its place by (fileid.ErrNoFileIndex, one value with
// write.ErrNoFileIndex, so errors.Is holds with it): a WebDAV or SMB drive, whose files read a zero
// index, or ReFS, whose index comes back all-ones.
func indexHeld(f *os.File, name string) error {
	switch ok, err := fileid.Indexed(f, false); {
	case err != nil:
		return err
	case !ok:
		return fmt.Errorf("%s: %w", name, fileid.ErrNoFileIndex)
	}
	return nil
}

// indexSame refuses name, one name in root's folder, where what is there has no file index
// os.SameFile can tell it apart from another by. collide compares Lstat's looks, so a link is the
// link itself: name is opened by the folder's handle, following no link, with FILE_READ_ATTRIBUTES
// alone and sharing all, so a link that leads out or nowhere and a file another process holds are
// each opened. What is opened must be held, collide's look, so the index tested is of the file
// compared, and a name changed since the look is refused.
func indexSame(root *os.Root, name string, held os.FileInfo) error {
	refused := func(err error) error {
		return &fs.PathError{Op: opCollide, Path: filepath.ToSlash(name), Err: err}
	}
	d, err := root.Open(".")
	if err != nil {
		return err
	}
	defer d.Close() //nolint:errcheck // read only; nothing is written through it
	h, err := winapi.OpenAt(windows.Handle(d.Fd()), name, winapi.How{Access: windows.FILE_READ_ATTRIBUTES,
		Share: windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE, Disposition: windows.FILE_OPEN, Attributes: windows.OBJ_CASE_INSENSITIVE})
	if err != nil {
		return refused(winapi.Dos(err))
	}
	f := os.NewFile(uintptr(h), name) // owns h, and closes it
	defer f.Close()                   //nolint:errcheck // read only; the file index was already read
	got, err := f.Stat()
	if err != nil {
		return refused(err)
	}
	if !os.SameFile(got, held) {
		return refused(errors.New("it changed between its look and the read of its file index"))
	}
	return indexHeld(f, name)
}

// indexRoot opens the folder root stands for, through root itself (following no link out of it), for
// its file index alone, checks it is the one the site looked at (its Stat against held, refusing
// `changed` on a mismatch), and refuses a no-index volume.
func indexRoot(root *os.Root, held os.FileInfo, name string, changed error) error {
	d, err := root.Open(".")
	if err != nil {
		return err
	}
	defer d.Close() //nolint:errcheck // read only; the file index was already read
	got, err := d.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(got, held) {
		return changed
	}
	return indexHeld(d, name)
}

// indexAtPath opens path for its file index alone, following its links (the sites that use it compare
// the resolved path by file ID, so a link to a folder names the folder), with FILE_READ_ATTRIBUTES and
// FILE_FLAG_BACKUP_SEMANTICS so a folder opens as a file does. It first checks the opened file is the
// one the site looked at, its Stat against held, and refuses `changed` on a mismatch, so the index it
// then tests is of the file compared, not of one swapped in since the look; and it refuses a no-index
// volume.
func indexAtPath(path string, held os.FileInfo, name string, changed error) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	h, err := winapi.CreateFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(h), path) // owns h, and closes it
	defer f.Close()                   //nolint:errcheck // read only; the file index was already read
	got, err := f.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(got, held) {
		return changed
	}
	return indexHeld(f, name)
}

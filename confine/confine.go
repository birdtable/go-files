// Package confine keeps a path beneath a folder and off every link on the way.
//
// It holds only what os.Root does not do: os.Root follows a relative link that stays
// inside its folder, and refuses an absolute one wherever it leads, and confine follows none.
// Local takes a path from outside, NoLinks walks a name by handle, Open opens with no link
// followed, ReadCapped reads a regular file so opened up to a cap, IsRunnable says from a file so
// opened, and IsRunnableFile from any open file, whether this system would run it, Names and
// NamesFile how many names a file so opened has,
// by hard links, RenameNoReplace and RenameExchange move names in one step under the same
// refusals, LinkFolder makes a link to a folder, a junction on Windows, and CopyTree copies a tree
// root to root, making each link inside it again and refusing one that leads outside.
// The README says how, on each system.
//
// On Windows a path that names a device or a pipe, not a path on a drive or a volume (\\.\, or
// \\?\ or \??\ naming neither a drive nor a Volume{GUID}), is refused before anything is opened,
// and each look at a path by its name, Resolve's walk and Below's, Nearest's and Folds' among them,
// asks for identification alone, so a pipe a link leads to learns who the caller is and may act as
// it in nothing. A UNC path (\\host\share\...) is not refused: a share is a place for files, and an
// open of one connects to the named host and authenticates the caller to it. A program that takes
// paths from another account refuses UNC paths itself, where filepath.VolumeName starts with \\.
//
// Built on os.Root, and golang.org/x/sys for openat2 on Linux and the Windows calls. It adds no
// link followed at any component, renames that never replace or that swap, folders made by handle
// and a link to a folder. The program configures the root, each name, and each call's flags and
// mode.
//
// It imports the standard library, golang.org/x/sys, golang.org/x/text/unicode/norm and internal/fileid, and on Windows internal/winapi.
package confine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	opWalk   = "walk"
	opOpen   = "open"
	opRename = "rename"
)

// ErrOutside is matched by every refusal of a path: one that leaves the
// root or names nothing in it, a link or junction on the way, a file swapped
// between the walk and the open, and a second name of a file opened to be
// written or moved.
var ErrOutside = errors.New("the path leads outside the folder, or through a link")

// ErrChanged is Resolve's and Below's refusal of a path that changed while it was walked: the
// system's own look at it found another file than the walk, or something where the walk found
// nothing, or a folder of the walk was another file when its file index was read. A missing name
// made by another process while the path was walked is refused so, as is a link swapped. It tells
// "the path changed under the walk" from "the path leads outside", never that the change was
// harmless. It matches ErrOutside.
var ErrChanged = fmt.Errorf("the path changed while it was walked: %w", ErrOutside)

// ErrNotEmpty is the refusal of Mkdir, MkdirPrivate, MkdirShared and MkdirFresh of a folder the
// call made that holds something by the time it is opened and checked: another process put a
// name in it, or swapped in a folder that holds one. Nothing was done to the folder. A folder of
// another account's, a link, and anything that is not a folder are refused with ErrOutside alone.
// It matches ErrOutside.
var ErrNotEmpty = fmt.Errorf("the folder this call made is not empty now: %w", ErrOutside)

// ErrNoExchange is RenameExchange's refusal where the system or the file
// system cannot swap two names in one step; nothing was moved. It matches
// errors.ErrUnsupported.
var ErrNoExchange = fmt.Errorf("two names cannot be swapped in one step here, and nothing was moved: %w", errors.ErrUnsupported)

// ErrNoFileLink is LinkFile's refusal where no symbolic link may be made here: on Windows by an
// account without the privilege, as one without Developer Mode, and on a system with no call that
// makes a link from a folder held open. Nothing was made. It matches errors.ErrUnsupported.
var ErrNoFileLink = fmt.Errorf("no symbolic link to a file may be made here, and nothing was made: %w", errors.ErrUnsupported)

// ErrNoMode is Mkdir's refusal on Windows of a perm the owner-only list
// would give more than: one without owner read, write and search (0o700),
// or with a bit beyond the nine permission bits. A folder there has an
// access list, not a mode. Nothing was made. It matches
// errors.ErrUnsupported, as write.ErrNoMode does.
var ErrNoMode = fmt.Errorf("a folder here has an access list and no mode, and the owner-only list it is made with cannot give this mode: %w", errors.ErrUnsupported)

// ErrNoAccessList is Mkdir's refusal where Windows would not make a folder this account's
// alone: a volume that keeps no access lists (FAT, exFAT), one that will not say whether it
// does, a share that will not take this account as the owner, or a folder where this account
// cannot open by name what it makes there with that list, as under a restricted token or an
// AppContainer the list does not name, or on a share that knows this account by another SID,
// which a throwaway folder made and removed first finds. The cause follows the text, and a
// denial matches fs.ErrPermission too. Nothing more is made. It matches errors.ErrUnsupported.
var ErrNoAccessList = fmt.Errorf("the volume keeps no access lists, so a folder made here would not be this account's alone, and nothing was made: %w", errors.ErrUnsupported)

// Local is the name inside a root, in this system's form, of p: a clean,
// relative, slash-separated path, as fs.ValidPath has it, with no ".",
// backslash, colon or NUL in it, and no component ..namedfork in any case
// (a file's fork on macOS), so it means the same on every system. An empty
// p is the root itself, ".". Anything else is refused with an error matching
// ErrOutside. On Windows filepath.Localize refuses a reserved name, such as
// CON, too.
func Local(p string) (string, error) {
	if p == "" {
		return ".", nil
	}
	if p == "." || strings.ContainsAny(p, "\\:\x00") {
		return "", fmt.Errorf("%q: %w", p, ErrOutside)
	}
	if forked(p, "/") {
		return "", fmt.Errorf("%q names a file's fork, %s: %w", p, namedFork, ErrOutside)
	}
	name, err := filepath.Localize(p)
	if err != nil {
		return "", fmt.Errorf("%q: %w", p, ErrOutside)
	}
	return name, nil
}

// NoLinks walks name in root by handle, component by component: each is
// looked at with Lstat in the folder opened before it, and each folder is
// opened and checked to be the one looked at, so no component is resolved
// by name twice. It refuses name when any component, the last included, is
// a link or another reparse point, when a component before the last is not
// a folder, or when a folder changed between its look and its open. It
// answers what the last component is, or nil when the walk stopped at a
// missing one. name is clean and local, as Local gives it; "." is the root.
//
// A refusal is an *fs.PathError of op "walk" naming, slash-separated, the
// component it stopped at, so a caller finds it with errors.As. A link or a
// junction, a folder that changed, and a name that is not clean and local,
// match ErrOutside.
func NoLinks(root *os.Root, name string) (fs.FileInfo, error) {
	if err := clean(name); err != nil {
		return nil, err
	}
	w, err := walkTo(root, name, func(string) {})
	if err != nil || w.dir == nil {
		return nil, err
	}
	if err := w.dir.Close(); err != nil {
		return nil, err
	}
	return w.info, nil
}

// escapes is whether err is os.Root's refusal of a path that leads outside it. os.Root does not
// export that error, and gives the same one for "..", with no look at the disk.
func escapes(root *os.Root, err error) bool {
	if err == nil {
		return false
	}
	_, probe := root.Lstat("..")
	pe, ok := errors.AsType[*fs.PathError](probe)
	return ok && pe.Err != nil && errors.Is(err, pe.Err)
}

func clean(name string) error {
	if !filepath.IsLocal(name) || filepath.Clean(name) != name {
		return &fs.PathError{Op: opWalk, Path: filepath.ToSlash(name), Err: fmt.Errorf("it is not a clean name inside the root: %w", ErrOutside)}
	}
	if forked(name, string(filepath.Separator)) {
		return &fs.PathError{Op: opWalk, Path: filepath.ToSlash(name), Err: fmt.Errorf("it names a file's fork, %s, not a name in a folder: %w", namedFork, ErrOutside)}
	}
	return nil
}

// namedFork is refused on every system, in any case: macOS opens a.txt/..namedfork/rsrc as
// a.txt's resource fork, with no link on the way, while a walk finds a.txt is no folder.
const namedFork = "..namedfork"

func forked(name, sep string) bool {
	return slices.ContainsFunc(strings.Split(name, sep), func(p string) bool { return strings.EqualFold(p, namedFork) })
}

// Open opens name in root as root.OpenFile does, with no link followed at
// any component. It refuses, with an error matching ErrOutside, a link or
// junction at any component of name, a name that is not clean and local,
// and a file with a second name, a hard link, opened to be written: a hard
// link to a file elsewhere in the root is that file, and no walk sees it.
// A file whose count of names cannot be read is not opened to be written:
// that error comes back. A hard link made after the check is not seen.
// os.O_TRUNC empties the file only after every check, so a refused open
// writes nothing.
//
// The open never waits: on Unix it is made with O_NONBLOCK, so a pipe at
// the name, one swapped in after a caller looked included, opens at once
// to be read, or is refused at once (ENXIO) to be written with no reader,
// and the caller's check of what it opened refuses it. A regular file or a
// folder reads and writes as it would without.
//
// On Linux it is openat2, and only a kernel with no openat2 (ENOSYS) has
// Open walk instead: any other answer, a seccomp filter's EPERM included, is
// refused. On macOS it is openat with O_NOFOLLOW_ANY. Elsewhere it is the
// walk.
func Open(root *os.Root, name string, flag int, perm fs.FileMode) (*os.File, error) {
	if err := clean(name); err != nil {
		return nil, err
	}
	return open(root, name, flag|noWait, perm, func(string) {})
}

// linkKind is what a component no path is walked through is, or "". Go reports a Windows
// junction as irregular, not as a link, and os.Root refuses one, even to a folder beneath the root.
func linkKind(m fs.FileMode) string {
	if m&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
		return "a link or another reparse point"
	}
	return ""
}

// namesOf refuses f when flag opens it to be written and it has more than one name.
func namesOf(f *os.File, name string, flag int) error {
	if flag&(os.O_WRONLY|os.O_RDWR) == 0 {
		return nil
	}
	more, err := shared(f)
	if err != nil {
		return err
	}
	if more {
		return &fs.PathError{Op: opOpen, Path: filepath.ToSlash(name),
			Err: fmt.Errorf("it has another name, a hard link, and a file with another name is never opened to be written: %w", ErrOutside)}
	}
	return nil
}

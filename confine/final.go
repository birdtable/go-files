package confine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const opFinal = "final"

// Final answers the system's own name for the folder or file path opens, read from a handle open
// on it and never built from path's spelling, so two spellings of one folder answer the same
// bytes: on Windows an 8.3 short name and another case give the long name in the case it is
// stored in, and on macOS another case gives the stored case. A caller that keys a record by a
// path, a journal, a cache or a lock name, keys it by Final's answer.
//
// path is absolute, and is opened as the system opens it: every link before its last name is
// followed, and a link at its last name is not, so a link answers its own name, in the folder the
// system names. To name what a link leads to, pass Resolve's answer.
//
// Each system answers from its own call on the handle: Windows from GetFinalPathNameByHandle's
// normalized name with its drive, without the \\?\ prefix where the name is shorter than MAX_PATH
// and GetFullPathName gives it back unchanged, as it does not a name that ends in a dot or a space
// or a device name; macOS from fcntl's F_GETPATH; Linux from the link /proc/self/fd/<n>, read from
// its own handle, opened, as self/fd is, by openat2 crossing no mount and following no magic link,
// beneath a /proc held to a procfs's root by fstatfs's magic and its inode, as trust reads /proc.
// Off those three, and where the call fails or answers no absolute path, as on Linux with no
// /proc, with a /proc that is not a procfs's root, with a mount on the way to /proc/self/fd/<n> or
// on the link itself, or with no openat2 (before Linux 5.6, or refused by a seccomp filter), it
// refuses with an *fs.PathError matching errors.ErrUnsupported, and never fs.ErrNotExist. The
// answer is then looked at with
// os.Lstat and held against the open file by os.SameFile: a name that is no longer the file
// opened, renamed or removed between the open and the read, is refused with ErrChanged, which
// matches ErrOutside. On a Windows volume that gives no file index os.SameFile has nothing to
// compare, so there the answer is not held against the file.
//
// A file with a second name, by a hard link, answers on Linux and Windows the name it was opened
// by, as does a file reached through a bind mount on Linux. macOS's F_GETPATH answers whichever of
// a file's names any process looked at last, which may be in another folder than path's. On HFS+ a
// folder may have a second name too, as a Time Machine disk's folders do, and then every name
// beneath it, a volume mounted there too, answers by whichever of the folder's names was looked at
// last, and HFS+'s count of a folder's names counts its entries, so it cannot tell; a network or
// FUSE volume's server decides what is one folder. So on macOS a name is a key only on a volume
// that gives a folder no second name: Final answers only where the opened file's volume and the
// volume of every folder of the answer up to / are apfs, msdos or exfat (fstatfs's type name), and
// refuses a name on another volume, or beneath one, matching errors.ErrUnsupported. There, a file,
// not a folder, with more than one name, as APFS gives one, is refused, matching
// errors.ErrUnsupported: its count of names is read from the open file after the name, and again
// from the answer's name after that, and a count above one either time refuses it, so a second
// name linked and unlinked between the reads is counted.
// On Linux, in a folder that folds case (Folds tells it), the kernel answers the spelling the name
// was first looked up by since it entered its cache, which may not be the stored one, so a caller
// keying a name there folds the answer itself.
//
// A path that is not absolute is refused, matching fs.ErrInvalid and ErrOutside, and a name that is
// not there with the system's error, matching fs.ErrNotExist. On macOS, whose open for F_GETPATH
// needs read access, a file or folder this account may not read is refused with the system's
// error, matching fs.ErrPermission; Linux, opening with O_PATH, answers it. The answer is a name
// read now: a rename after it is not seen. And it is a key only for a path whose folders no other
// account or process renames while Final runs: a folder of the path renamed during the call and put
// back may have Final answer another name of what it opened, as on macOS through a folder with a
// second name. A caller that cannot promise that judges the path with trust.Path first.
func Final(path string) (string, error) {
	return final(path, finalOf, os.Lstat)
}

// final is Final with read, the system's answer for the open file, and lstat, the look at that
// answer's name, passed in so a test of this package hands it others.
func final(path string, read func(*os.File) (string, error), lstat func(string) (fs.FileInfo, error)) (string, error) {
	if !filepath.IsAbs(path) {
		return "", &fs.PathError{Op: opFinal, Path: path, Err: fmt.Errorf("it is not an absolute path: %w", errors.Join(fs.ErrInvalid, ErrOutside))}
	}
	f, err := openFinal(path)
	if err != nil {
		return "", &fs.PathError{Op: opFinal, Path: path, Err: err}
	}
	defer f.Close() //nolint:errcheck // read only; nothing is written through it
	name, err := read(f)
	switch {
	case err != nil:
		// The cause is kept as text, not wrapped: a call that fails with ENOENT, as a readlink
		// where /proc is not mounted does, says nothing of whether path is there.
		return "", &fs.PathError{Op: opFinal, Path: path, Err: fmt.Errorf("the system cannot say the name of what it opened (%v): %w", err, errors.ErrUnsupported)} //nolint:errorlint // the cause is shown, never matched: a /proc ENOENT is not the file gone
	case !filepath.IsAbs(name):
		return "", &fs.PathError{Op: opFinal, Path: path, Err: fmt.Errorf("the system named what it opened %q, which is not an absolute path: %w", name, errors.ErrUnsupported)}
	}
	if err := finalVolumes(f, name); err != nil {
		return "", &fs.PathError{Op: opFinal, Path: path, Err: err}
	}
	// held is read after the name, and got after held, and each is counted by finalHeld: a second
	// name that the name may be, linked and unlinked by another process between the reads, is
	// counted by one of them, as it is there when the name is read and again when got is.
	held, err := f.Stat()
	if err != nil {
		return "", &fs.PathError{Op: opFinal, Path: path, Err: err}
	}
	if err := finalHeld(held); err != nil {
		return "", &fs.PathError{Op: opFinal, Path: path, Err: err}
	}
	got, err := lstat(name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", &fs.PathError{Op: opFinal, Path: path, Err: err}
	}
	if err != nil || !os.SameFile(held, got) {
		return "", &fs.PathError{Op: opFinal, Path: path, Err: fmt.Errorf("%w: the system named what it opened %q, and that name is not the file opened now", ErrChanged, name)}
	}
	if err := finalHeld(got); err != nil {
		return "", &fs.PathError{Op: opFinal, Path: path, Err: err}
	}
	return name, nil
}

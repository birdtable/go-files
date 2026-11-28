package confine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/unix"
)

// errFinalLinks is Final's refusal on macOS of a file with more than one name: F_GETPATH answers
// whichever of its names any process looked at last, which may be in another folder than the
// path's, so the answer is not a key for the file.
var errFinalLinks = fmt.Errorf("it has more than one name, a hard link, and macOS names such a file by whichever name was looked at last: %w", errors.ErrUnsupported)

// finalType is whether t, fstatfs's type name, is a file system Final answers on, on macOS: apfs,
// which gives a folder no second name and answers a name's stored spelling. On any other, HFS+,
// where a folder may have one, as a Time Machine disk's folders do, a network share, a FUSE volume,
// or a type not listed, F_GETPATH may name a folder, and all beneath it, by whichever of its names
// any process looked at last; msdos and exfat answer the spelling a name was first looked up by
// since it entered the cache, not the stored one. APFS keeps its promises as the system writes it;
// a volume whose bytes another account wrote is judged with trust.Path first, as Final's doc says.
func finalType(t string) bool {
	return t == "apfs"
}

// finalVolumes refuses f, the file Final opened, unless its volume and the volume of every folder
// of name, the answer, up to /, are apfs, a finalType: a volume mounted beneath a folder with a second
// name is named through it, so the answer is no key unless every volume on the way is listed. A
// volume that cannot be read is refused too.
func finalVolumes(f *os.File, name string) error {
	conn, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var st unix.Statfs_t
	var callErr error
	if err := conn.Control(func(fd uintptr) { callErr = unix.Fstatfs(int(fd), &st) }); err != nil {
		return err
	}
	for at := "what it opened"; ; {
		if callErr != nil {
			return fmt.Errorf("the volume of %s cannot be read (%v): %w", at, callErr, errors.ErrUnsupported) //nolint:errorlint // the cause is shown, never matched
		}
		if t := unix.ByteSliceToString(st.Fstypename[:]); !finalType(t) {
			return fmt.Errorf("%s is on a volume of type %q, and macOS answers a name only on apfs, which gives a folder no second name and answers its stored spelling: %w", at, t, errors.ErrUnsupported)
		}
		if name == "/" {
			return nil
		}
		name = filepath.Dir(name)
		at = name + ", a folder of the name answered"
		callErr = unix.Statfs(name, &st)
		for errors.Is(callErr, unix.EINTR) {
			callErr = unix.Statfs(name, &st)
		}
	}
}

// finalHeld refuses held, the file Final opened, read after its name, where it is not a folder and
// has more than one name. A folder is answered: finalVolumes answers only on apfs, which gives a
// folder no second name.
func finalHeld(held fs.FileInfo) error {
	if held.IsDir() {
		return nil
	}
	n, err := linkCount(nil, held)
	if err != nil {
		return err
	}
	if n > 1 {
		return errFinalLinks
	}
	return nil
}

// finalFlags opens for the name alone: O_EVTONLY asks for neither reading nor writing, though the
// kernel still wants read access, so a name this account may not read is EACCES; O_SYMLINK opens a
// link at the last name itself; O_NONBLOCK keeps a pipe from waiting on a writer, and O_NOCTTY a
// terminal from becoming this process's.
const finalFlags = unix.O_EVTONLY | unix.O_SYMLINK | unix.O_NONBLOCK | unix.O_NOCTTY | unix.O_CLOEXEC

// openFinal opens path for its name alone, following no link at its last name.
func openFinal(path string) (*os.File, error) {
	fd, err := unix.Open(path, finalFlags, 0)
	for errors.Is(err, unix.EINTR) {
		fd, err = unix.Open(path, finalFlags, 0)
	}
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

// finalOf asks the kernel f's name with fcntl's F_GETPATH, into a buffer of MAXPATHLEN bytes.
//
// golang.org/x/sys exports no fcntl that takes a buffer, and a raw system call is refused here, so
// the call goes through FcntlFstore, libSystem's fcntl with a pointer argument: the buffer is
// handed to it as an *Fstore_t, smaller than the buffer and 8-aligned as it is, and F_GETPATH
// writes the name into the buffer behind it.
func finalOf(f *os.File) (string, error) {
	conn, err := f.SyscallConn()
	if err != nil {
		return "", err
	}
	var buf [unix.PathMax / 8]uint64
	var callErr error
	if err := conn.Control(func(fd uintptr) {
		callErr = unix.FcntlFstore(fd, unix.F_GETPATH, (*unix.Fstore_t)(unsafe.Pointer(&buf))) //nolint:gosec // G103: F_GETPATH writes the name into buf, MAXPATHLEN bytes, for the one call
	}); err != nil {
		return "", err
	}
	if callErr != nil {
		return "", callErr
	}
	return unix.ByteSliceToString(unsafe.Slice((*byte)(unsafe.Pointer(&buf)), unix.PathMax)), nil //nolint:gosec // G103: buf's bytes, MAXPATHLEN of them
}

package write

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// linkFolder makes at, beneath root, a junction to the folder target, as confine.LinkFolder makes
// one on Windows, with no privilege; write's tests do not import confine.
func linkFolder(t *testing.T, root *os.Root, target, at string) error {
	t.Helper()
	path := filepath.Join(root.Name(), at)
	if err := os.Mkdir(path, 0o700); err != nil {
		return err
	}
	reparse(t, path, junctionData(target))
	return nil
}

// umaskWrite is refused: Windows has no umask.
func umaskWrite(string, string, string) error { return errors.New("Windows has no umask") }

// umaskLog is refused: Windows has no umask.
func umaskLog(string, string, string) error { return errors.New("Windows has no umask") }

// permIs fails the test when there is no file at path. Windows does not apply Mode, so perm is not
// compared.
func permIs(t *testing.T, path string, _ os.FileMode) {
	t.Helper()
	if fi, err := os.Stat(path); err != nil {
		t.Errorf("%s is %v (%v)", path, fi, err)
	}
}

// keptAs fails t unless the file at path has what a copy of a file of the mode perm keeps on
// Windows: the owner-only list, and the read-only attribute where perm has no owner write.
func keptAs(t *testing.T, path string, perm os.FileMode) {
	t.Helper()
	ownerOnly(t, path)
	if got, want := isReadOnly(t, path), perm&0o200 == 0; got != want {
		t.Errorf("%s: read-only %v, and want %v, as the mode %#o says", path, got, want, perm)
	}
}

// holdShared takes a shared LockFileEx on the byte Append locks, through a handle opened only to
// read, as any reader of the log can, and answers the call that lets go, which the test's end makes
// if the test has not.
func holdShared(t *testing.T, path string) func() {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	letGo := func() { _ = f.Close() }
	t.Cleanup(letGo)
	if err := windows.LockFileEx(windows.Handle(f.Fd()), 0, 0, 1, 0, far()); err != nil { //nolint:forbidigo // register (path-lock): a reader's shared hold on Append's byte, which the test needs Append to refuse
		t.Fatal(err)
	}
	return letGo
}

// held is the third swap's act on the held log's name as the hard-link test wants it: refused, as
// Append pins the held log by its name against a removal and a rename over it, so its count sees
// two. A remove is refused with a sharing violation, a rename over the name with an access denial.
func held(act string, err error) error {
	if err == nil {
		return fmt.Errorf("%s: done, and want it refused", act)
	}
	if errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return nil
	}
	return fmt.Errorf("%s: %w, and want a sharing violation or an access denial", act, err)
}

// taken is a third swap's act whose own result the hard-link test does not judge on Windows, only
// Append's refusal: NTFS may refuse a part of the act, as the pin's share is the held file's, or
// carry the pin to the name that stays. What the act returned is logged for the run's record.
func taken(t *testing.T, act string, err error) error {
	t.Helper()
	t.Logf("%s: %v", act, err)
	return nil
}

// replaced is a replace row's act, a hard link made over the held log's name by linkOver, as the
// hard-link test wants it on Windows: done, or refused as the pin refuses it, which
// NtSetInformationFile gives as an access denial (plain) or a sharing violation (POSIX semantics).
// Any other failure is linkOver's own, the buffer's or the class's, and fails the row, so a broken
// helper reads red, not green. The NTSTATUS is compared as it is: x/sys gives it no
// Is, so it never matches a Win32 error.
func replaced(t *testing.T, act string, err error) error {
	t.Helper()
	t.Logf("%s: %v", act, err)
	if err == nil || errors.Is(err, windows.STATUS_ACCESS_DENIED) || errors.Is(err, windows.STATUS_SHARING_VIOLATION) {
		return nil
	}
	return fmt.Errorf("%s: %w, and want it done or refused by the pin", act, err)
}

// fileLinkInformation is the FILE_INFORMATION_CLASS FileLinkInformation, which takes
// ReplaceIfExists. x/sys's windows.FileLinkInformation is 72, the class FileLinkInformationEx,
// which takes the FILE_LINK_ flags.
const fileLinkInformation = 11

// linkOver makes a hard link to the file at from at the name to, over the file there, through
// NtSetInformationFile with the replace os.Link and os.Root.Link never ask: plain by
// FileLinkInformation and ReplaceIfExists, or, with posix, by FileLinkInformationEx and
// FILE_LINK_REPLACE_IF_EXISTS with FILE_LINK_POSIX_SEMANTICS. The handle on from is opened as
// CreateHardLink opens it, to write its attributes and sharing all.
//
// FILE_LINK_INFORMATION and FILE_LINK_INFORMATION_EX share one layout, written here field by field:
// a ULONG, ReplaceIfExists in its low byte or the FILE_LINK_ flags; the handle of the folder the
// name is beneath, none here; at twice a pointer's size, the name's length in bytes as a ULONG; and
// after it the name, of that length, with no NUL.
func linkOver(from, to string, posix bool) error {
	p, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(p, windows.FILE_WRITE_ATTRIBUTES|windows.SYNCHRONIZE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	name := utf16.Encode([]rune(`\??\` + to))
	length := 2 * bits.UintSize / 8
	b := make([]byte, length+4+2*len(name))
	class, flags := uint32(fileLinkInformation), uint32(1) // ReplaceIfExists
	if posix {
		class, flags = windows.FileLinkInformation, windows.FILE_LINK_REPLACE_IF_EXISTS|windows.FILE_LINK_POSIX_SEMANTICS
	}
	binary.LittleEndian.PutUint32(b, flags)
	binary.LittleEndian.PutUint32(b[length:], uint32(2*len(name)))
	for i, c := range name {
		binary.LittleEndian.PutUint16(b[length+4+2*i:], c)
	}
	return windows.NtSetInformationFile(h, new(windows.IO_STATUS_BLOCK), &b[0], uint32(len(b)), class)
}

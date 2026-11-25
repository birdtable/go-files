package trusttest

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/birdtable/go-files/trust"
)

// SystemFile answers a file the system owns, which this account neither owns nor may open to
// write, and its owner as trust.OwnerOf reads it and trust.ReadOwned takes it: "uid:N" on Unix
// and the SID on Windows. A test of a program that must refuse, or must not
// change, a file of another account's has one with no root and no file of its own to make.
//
// It is found, never named: the first of this system's candidates that is a regular file, named
// with no link on the way to it, opened to read, owned by an account other than this one, and
// whose open to write this account is refused. The candidates are, on Windows, ntdll.dll in the
// folder GetSystemDirectory answers, never one the environment names, which TrustedInstaller owns
// and Administrators may not write; on macOS /private/etc/hosts and /usr/bin/true; on Linux and
// another Unix /etc/passwd and /bin/sh. The owner answered is the one read, so in a Linux user
// namespace that does not map root, as go-testkit's contain makes, a file of root's is answered
// owned by the kernel's overflow account, uid:65534 by default, and not uid:0: a test that takes
// the owner to be root's is what this answer catches.
//
// Every candidate is tried first, whoever runs the test. Where none passes, the test is skipped
// when this account is the system's own, uid:0 or SYSTEM (S-1-5-18), which may write every one, so
// the run shows the proof not made, and failed otherwise; each says this account and every
// candidate with why it did not pass. Being in Administrators is not the system's own account.
func SystemFile(t testing.TB) (path, owner string) {
	t.Helper()
	cands, err := candidates()
	if err != nil {
		t.Fatalf("trusttest.SystemFile: this system's candidates: %v", err)
	}
	me, err := trust.Account()
	if me == "" {
		t.Fatalf("trusttest.SystemFile: this account: %v", err)
	}
	return systemFile(t, cands, me, openWrite)
}

// systemFile is SystemFile over cands, as the account me, learning by write whether me may open a
// candidate to write.
func systemFile(t testing.TB, cands []string, me string, write func(string) error) (path, owner string) {
	t.Helper()
	whys := make([]string, 0, len(cands))
	for _, c := range cands {
		owner, err := systemOwned(c, me, write)
		if err == nil {
			return c, owner
		}
		whys = append(whys, fmt.Sprintf("%s: %v", c, err))
	}
	said := strings.Join(whys, "; ")
	if me == "uid:0" || me == "S-1-5-18" { // root and SYSTEM; Administrators is not the system's own
		t.Skipf("trusttest.SystemFile: no candidate is a file of another account's this account may not write, and this account is %s, the system's own, which may write each, so none is proved: %s", me, said)
	}
	t.Fatalf("trusttest.SystemFile: no candidate is a file of another account's that this account, %s, may not write: %s", me, said)
	return "", ""
}

// systemOwned is the owner of the file path, or why it is no file of another account's that me may
// not write: a link on the way to it, an open to read that fails, a name that is not a regular
// file, an owner that cannot be read or is me, and an open to write, by write, that is not refused.
func systemOwned(path, me string, write func(string) error) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if !samePath(real, path) {
		return "", fmt.Errorf("a link is on the way to it, which leads to %s", real)
	}
	f, err := openRead(path)
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck // a file only read
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("it is not a regular file, but %v", info.Mode().Type())
	}
	owner, err := trust.OwnerOf(f)
	if err != nil {
		return "", err
	}
	if owner == me {
		return "", fmt.Errorf("this account, %s, owns it", me)
	}
	switch err := write(path); {
	case err == nil:
		return "", fmt.Errorf("this account, %s, opens it to write, though %s owns it", me, owner)
	case !errors.Is(err, fs.ErrPermission):
		return "", fmt.Errorf("an open of it to write failed with %w, not a refusal of this account", err)
	}
	return owner, nil
}

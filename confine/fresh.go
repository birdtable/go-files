package confine

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// freshBytes is how many random bytes end a name MkdirFresh picks: 64 bits, written as 16
// lower-case hex digits, which fold to themselves on every system.
const freshBytes = 8

// freshTries is how many names MkdirFresh tries before it refuses: a taken name is tried again under
// another, and 64 random bits are taken by chance once in billions of folders, so each try past
// the first is a name planted by whoever guessed it.
const freshTries = 16

// MkdirFresh makes one new folder in root, under a name it picks, and answers it opened as a root
// of its own, with its name. The folder is MkdirPrivate's in every respect: 0700 exactly whatever
// the umask on Unix, and on Windows the owner-only list, held once it is opened as Mkdir holds it;
// a volume that keeps no access lists is refused with ErrNoAccessList, which matches
// errors.ErrUnsupported, and nothing is made. It is never a looser folder.
//
// The name is prefix and then 64 bits from crypto/rand in 16 lower-case hex digits. prefix is the
// start of one name and may be empty; the whole name must pass Portable, so a prefix with a
// separator or a character Windows reserves, one that makes the name longer than 255 bytes, and a
// prefix that is .git in any case, are refused with an error matching fs.ErrInvalid before
// anything is made.
//
// The folder is fresh: this call made it, by a make that fails on a name that is taken, a link
// among them. A taken name is never opened, followed or answered; it is tried again under another
// name, and after 16 names taken the call is refused with an error matching fs.ErrExist, and
// nothing is made. One level alone is made: a missing folder above it is MkdirPrivate's. A folder
// swapped in at the name between its make and its open is refused with ErrOutside, as Mkdir
// refuses one, and nothing is removed. Removing the folder, or moving it into place with
// RenameNoReplace(root, name, to), is the caller's.
//
// What is not promised: the folder is the owner's alone, but its name is root's. In a root another
// account can write, that account can rename or remove the name. The *os.Root answered stays the
// folder that was made. A caller that needs the name to stay judges root with trust first.
func MkdirFresh(root *os.Root, prefix string) (dir *os.Root, name string, err error) {
	return mkdirFresh(root, prefix, randomHex, func(string) {}, false)
}

// randomHex is freshBytes from crypto/rand, in lower-case hex. crypto/rand's Read does not fail: a
// system that cannot give random bytes ends the program.
func randomHex() string {
	var b [freshBytes]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// mkdirFresh is MkdirFresh, each name's random part from random, so a test of this package
// answers a name it planted, calling reached at Mkdir's steps, so the test swaps there, and with
// listless, which Mkdir's tests set (mkdir).
func mkdirFresh(root *os.Root, prefix string, random func() string, reached func(step string), listless bool) (*os.Root, string, error) {
	if err := freshPrefix(prefix); err != nil {
		return nil, "", &fs.PathError{Op: "mkdir", Path: prefix, Err: err}
	}
	if err := rootOnPipes(root, prefix); err != nil && root == nil {
		return nil, "", err
	}
	w := walk{perm: 0o700, kind: ownerOnly, reached: reached, listless: listless}
	for range freshTries {
		name := prefix + random()
		if _, err := Portable(name); err != nil { // random's own, which hex always passes
			return nil, "", &fs.PathError{Op: "mkdir", Path: name, Err: err}
		}
		err := newFolder(root, name, w.perm, w.kind, w.listless)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		w.reached(stepMadeFolder)
		dir, err := into(root, name, name, true, w)
		if err != nil {
			return nil, "", err
		}
		if err := flushRoot(root); err != nil {
			return nil, "", errors.Join(err, dir.Close())
		}
		return dir, name, nil
	}
	return nil, "", &fs.PathError{Op: "mkdir", Path: prefix, Err: fmt.Errorf("each of the %d names tried was taken, and nothing was made: %w", freshTries, fs.ErrExist)}
}

// freshPrefix refuses, with an error matching fs.ErrInvalid, a prefix no name MkdirFresh picks can
// pass Portable with, or one that is .git in some case. Whether Portable passes the name does not
// hang on its hex digits: 16 of them make it no 8.3 short name and no device's, and a hex digit
// composes with nothing.
func freshPrefix(prefix string) error {
	if _, err := Portable(prefix + strings.Repeat("0", 2*freshBytes)); err != nil {
		return err
	}
	if prefix != "" && portableKey(prefix) == ".git" {
		return fmt.Errorf("the prefix %q is .git, in some case: %w", prefix, fs.ErrInvalid)
	}
	return nil
}

package trust

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// privateTries is how many times MkdirPrivate judges and makes dir before it refuses. A try is
// cut short only by another make of dir by this account, which the next try finds and judges.
const privateTries = 8

// The steps of MkdirPrivate where another process could make or change a folder, for a test's
// reached.
const (
	stepFound  = "between the look for the nearest part and the judge of the folders above"
	stepWalked = "between the judge of the folders above and the look at the folder it reached"
	stepJudged = "between the judge of the folders above and the make"
	stepMade   = "between the make and the judge of dir"
	stepDir    = "between the judge of dir and the judge of the folder answered"
)

// MkdirPrivate answers dir, an absolute, clean path, as a root of its own: a folder only this
// account reaches, judged by Path for who before anything is made and again once it is there.
//
// The nearest part of dir that is there is found by os.Lstat of each name as written, so a link is
// a name that is there. Path walks it and the first name below it that is not, judging each folder
// above as a folder above the last name, a sticky folder of root's as /tmp included, and following
// a link only where it trusts it, none with who.NoLinks: nothing is made through a name it refuses
// then. One changed after, as another account may in a sticky folder, is refused by the judge of
// dir, the folders made below it left there, empty. Each missing folder is made below the folder
// the walk reached, with no link in it, by confine.MkdirPrivate: private as it is made, 0700 on
// Unix, the owner-only list on Windows. dir is then judged by Path as a PrivateFolder, the folder
// answered by Held, and the two must be one file (os.SameFile). A folder that was there keeps its
// mode: one not private is refused as Path refuses it, with its Fix, never changed.
//
// Another make of dir by this account at the same time is not refused. A folder it made between
// this call's judge and its make, a name it put in a folder this call made (confine.ErrNotEmpty),
// and a folder made but not there to a second look, as Windows answers for a moment, send this
// call back to the judge, which finds the folder there and judges it as one that was there. After
// 8 tries cut short so, it is refused with ErrReplaced.
//
// who is both walks'. RootOnly trusts no folder this account makes, so it is refused before
// anything is read, matching ErrNotOwners. A refusal is Path's or Held's, or confine's, naming the
// folder below which it made.
func MkdirPrivate(dir string, who Who) (*os.Root, error) { return mkdirPrivate(dir, who, nil) }

// mkdirPrivate is MkdirPrivate, calling reached at each of its steps, run only under
// testing.Testing(), so a test of this package makes or changes a folder there.
func mkdirPrivate(dir string, who Who, reached func(step string)) (*os.Root, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return nil, refuse(dir, strconv.Quote(dir)+" is not an absolute, clean path", Fix{}, nil)
	}
	if who.RootOnly {
		return nil, refuse("", "the setting RootOnly trusts no folder this account makes", Fix{}, ErrNotOwners)
	}
	at := func(step string) {
		if reached != nil && testing.Testing() {
			reached(step)
		}
	}
	for range privateTries {
		root, again, err := privateTry(dir, who, at)
		if !again {
			return root, err
		}
	}
	return nil, refuse(dir, dir+" was made or changed by another make at each of "+strconv.Itoa(privateTries)+" tries", Fix{}, ErrReplaced)
}

// privateTry is one try of MkdirPrivate. again is whether another make of dir cut it short.
func privateTry(dir string, who Who, at func(string)) (_ *os.Root, again bool, _ error) {
	top, rest, err := nearestPart(dir)
	if err != nil {
		return nil, false, err
	}
	var made *os.Root
	if len(rest) > 0 {
		at(stepFound)
		base, again, err := above(top, rest[0], who, at)
		if err != nil || again {
			return nil, again, err
		}
		at(stepJudged)
		if made, err = makeBelow(base, rest); err != nil {
			return nil, raced(err), err
		}
		at(stepMade)
	}
	got, err := Path(dir, PrivateFolder, who)
	if err == nil && made == nil {
		made, err = os.OpenRoot(got)
	}
	if err == nil {
		at(stepDir)
		err = sameHeld(made, got, who)
	}
	if err != nil {
		if made != nil {
			err = errors.Join(err, made.Close())
		}
		return nil, errors.Is(err, fs.ErrNotExist), err
	}
	return made, false, nil
}

// raced is whether err, confine's refusal of the make, is another make's: a name put in a folder
// this call made (confine.ErrNotEmpty), or a folder not there to a second look.
func raced(err error) bool {
	return errors.Is(err, confine.ErrNotEmpty) || errors.Is(err, fs.ErrNotExist)
}

// nearestPart is the longest part of dir that is there, each name read by os.Lstat, and the names
// below it, in order.
func nearestPart(dir string) (string, []string, error) {
	top, rest := dir, []string(nil)
	for {
		_, err := os.Lstat(top)
		up := filepath.Dir(top)
		switch {
		case err == nil:
			slices.Reverse(rest)
			return top, rest, nil
		case !errors.Is(err, fs.ErrNotExist) || up == top:
			return "", nil, err
		}
		rest, top = append(rest, filepath.Base(top)), up
	}
}

// above judges the folders above first, top's name below it that was not there, by Path's walk of
// it, and answers the folder the walk reached for top, with no link in it. first there by now is
// again, for the next try. A walk cut short by anything but a name not there, or ending in another
// folder than top, as when top or a link in it changed after the walk, is answered as it refused.
func above(top, first string, who Who, at func(string)) (string, bool, error) {
	_, err := Path(filepath.Join(top, first), Folder, who)
	at(stepWalked)
	if err == nil {
		return "", true, nil
	}
	pe, ok := errors.AsType[*fs.PathError](err)
	if !ok || !errors.Is(err, fs.ErrNotExist) {
		return "", false, err
	}
	base := filepath.Dir(pe.Path)
	t, terr := os.Stat(top)
	b, berr := os.Stat(base)
	if terr != nil || berr != nil || !os.SameFile(t, b) {
		return "", false, err
	}
	return base, false, nil
}

// makeBelow makes each folder of rest below base by confine.MkdirPrivate, and answers the last.
func makeBelow(base string, rest []string) (*os.Root, error) {
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	made, err := confine.MkdirPrivate(root, filepath.Join(rest...))
	if err != nil {
		err = fmt.Errorf("making below %s: %w", base, err)
	}
	if cerr := root.Close(); cerr != nil {
		if made != nil {
			cerr = errors.Join(cerr, made.Close())
		}
		return nil, errors.Join(err, cerr)
	}
	return made, err
}

// sameHeld holds the folder root is to be the file at got, the path Path answered, then judges it,
// on its own handle, as a PrivateFolder.
func sameHeld(root *os.Root, got string, who Who) (err error) {
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	have, err := f.Stat()
	if err != nil {
		return err
	}
	named, err := os.Lstat(got)
	if err != nil {
		return err
	}
	if !os.SameFile(have, named) {
		return refuse(got, got+" is another folder than the one made or opened: it changed after it was judged", Fix{}, ErrReplaced)
	}
	return Held(f, PrivateFolder, who)
}

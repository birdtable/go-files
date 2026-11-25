//go:build unix || windows

package appdirs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/trust"
)

// Temp is the running account's own temp folder for the program app, made 0700 when it is not
// there, on Windows with an access list that only this account and SYSTEM reach, none of the
// folder above's inherited, which what is made in it inherits (a volume that keeps no access
// lists is refused with ErrNoAccessList, and nothing is made), and answered with no link in it: <temp>/<app>-<uid> on Unix, <uid> the effective one, and
// <temp>\<app> on Windows, whose temp folder is the account's already. <temp> is os.TempDir read
// through its links once trust has judged each of them, as what it names; from there down, trust
// judges every name of the way as this account's, with no link (Who.NoLinks), before the folder is
// made and again after it, so a folder swapped for a link between the check and the make is
// refused, never answered. Of each of who only Apart, FileSystems and Mounts are read, joined.
// Each of Apart must be an absolute, clean path to a folder that is there: Temp refuses, with an
// error matching trust.ErrInside, a temp folder that is one of them, lies below one, or holds one.
// FileSystems and Mounts are trust's, for both walks, so a temp folder on a disk mounted nosuid,
// as a hardened /tmp, /var/tmp or /home is, or noexec without nosuid, passes once a folder of
// Mounts is on it.
// A temp folder right in root's sticky folder, as /tmp, passes only on that folder's device,
// whoever owns it, and root's there passes as root's own, as trust judges both. The answer is a
// path judged once, and not kept: call Temp again before each use.
func Temp(app string, who ...trust.Who) (string, error) { return tempOf(app, who, false) }

// TempOverflow is Temp for a test in a Linux user namespace that maps this account and not root,
// where / and the temp folder above this account's show as the kernel's overflow account: both of
// Temp's walks set trust.Who.Overflow, so trust trusts that account as root, and judge every other
// rule as Temp's do. It is for tests alone. Outside a test binary in such a
// namespace it refuses, with an error matching trust.ErrOverflow, as trust does, and off Linux
// with one matching errors.ErrUnsupported too; nothing is made. Temp never sets Who.Overflow, and
// TempOverflow reads only Apart, FileSystems and Mounts of who, as Temp does.
func TempOverflow(app string, who ...trust.Who) (string, error) { return tempOf(app, who, true) }

// tempOf is Temp, who joined, with trust.Who.Overflow set in both walks where overflow is.
func tempOf(app string, who []trust.Who, overflow bool) (string, error) {
	if !plain(app) {
		return "", fmt.Errorf("%q: %w", app, ErrApp)
	}
	w := trust.Who{NoLinks: true, Overflow: overflow}
	for _, o := range who {
		w.Apart = append(w.Apart, o.Apart...)
		w.FileSystems = append(w.FileSystems, o.FileSystems...)
		w.Mounts = append(w.Mounts, o.Mounts...)
	}
	name := tempName(app)
	base, err := tempBase(os.TempDir(), name, trust.Who{FileSystems: w.FileSystems, Mounts: w.Mounts, Overflow: overflow})
	if err != nil {
		return "", err
	}
	return tempAs(filepath.Join(base, name), w, nil, false)
}

// tempBase is t, the system's temp folder, as what it names, each link of its way judged before it
// is read: trust walks t's folder name with links followed (no NoLinks), so each link passes
// trust's own rule (its owner, its access list, the hold right in a sticky folder) before its
// target is read, and the walk ends below t, since a sticky folder as the last name is refused.
// name not there is that walk's pass. t is then read through the links the walk passed, which only
// an account trust trusts can change, and temp judges the whole answer again with no link. who is
// the walk's: Temp's FileSystems and Mounts, and for TempOverflow Overflow.
func tempBase(t, name string, who trust.Who) (string, error) {
	if _, err := trust.Path(filepath.Join(t, name), trust.PrivateFolder, who); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return realPath(t)
}

// temp is Temp's folder dir. between, set by a test alone and run only under testing.Testing(),
// runs between the check and the make, where another account would swap a folder of the way.
// listless, set by a test alone and read only under testing.Testing() (in winapi), takes every
// volume as one that keeps no access lists.
func temp(dir string, apart []string, between func(), listless bool) (string, error) {
	return tempAs(dir, trust.Who{NoLinks: true, Apart: apart}, between, listless)
}

// tempAs is temp, its walks judging by who, which holds NoLinks and the apart folders, and for
// TempOverflow Overflow.
func tempAs(dir string, who trust.Who, between func(), listless bool) (string, error) {
	apart := who.Apart
	for _, a := range apart {
		if _, err := os.Stat(a); err != nil { // before anything is made
			return "", err
		}
	}
	_, err := trust.Path(dir, trust.PrivateFolder, who)
	switch {
	case missing(err, dir):
		// The walk judged every folder above dir as it judges one above the last name, and ended
		// at dir, which is not there: that is its pass. Mkdir makes no name that is there by now.
		if between != nil && testing.Testing() {
			between()
		}
		if err := newFolder(dir, listless); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	case err != nil:
		return "", err
	}
	got, err := trust.Path(dir, trust.PrivateFolder, who)
	if err != nil {
		return "", err
	}
	if err := holdsNone(got, apart); err != nil {
		return "", err
	}
	return got, nil
}

// missing is whether err is the walk's pass at dir: a name not there, and the name is dir itself.
// A name not there above dir, or one the walk read for an apart folder, is a refusal, so nothing is
// made.
func missing(err error, dir string) bool {
	var pe *fs.PathError
	return errors.Is(err, fs.ErrNotExist) && errors.As(err, &pe) && pe.Path == dir
}

// holdsNone refuses dir when it holds a folder of apart: each one's way, as written and as its links
// resolve, is read by file ID, with no judgement, and a folder of it that is dir is ErrInside.
func holdsNone(dir string, apart []string) error {
	d, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if err := indexAtPath(dir, d, dir, changedWalk(dir)); err != nil {
		return err
	}
	for _, a := range apart {
		real, err := realPath(a)
		if err != nil {
			return err
		}
		for _, p := range []string{a, real} {
			on, err := onWay(p, d)
			if err != nil {
				return err
			}
			if on {
				return fmt.Errorf("the temp folder %s holds %s, a folder it is kept apart from: %w", dir, a, trust.ErrInside)
			}
		}
	}
	return nil
}

// onWay is whether d is p or a folder above it, each read through its links by file ID.
func onWay(p string, d os.FileInfo) (bool, error) {
	for ; ; p = filepath.Dir(p) {
		f, err := os.Stat(p)
		if err != nil {
			return false, err
		}
		if err := indexAtPath(p, f, p, changedWalk(p)); err != nil {
			return false, err
		}
		if os.SameFile(f, d) {
			return true, nil
		}
		if filepath.Dir(p) == p {
			return false, nil
		}
	}
}

// changedWalk is the refusal when a folder on the apart-walk is not the one looked at when its file
// index is read, so it changed under the walk; nothing is made.
func changedWalk(path string) error {
	return fmt.Errorf("%s changed between its look and the read of its file index, and nothing was made: %w", path, fs.ErrInvalid)
}

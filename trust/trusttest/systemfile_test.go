package trusttest_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/trust"
	"github.com/birdtable/go-files/trust/trusttest"
)

// other is an account that owns no file of the test's, as the account SystemFileAs runs as where
// the test's own file must pass as another account's.
const other = "S-1-5-21-1-2-3-4000"

// refused is a write that this account is refused, as a system file's open to write is.
func refused(string) error { return fs.ErrPermission }

// tempFile is a regular file of the test's, named with no link on the way to it, as a temp folder
// on macOS, beneath /var, is not until it is resolved.
func tempFile(t *testing.T, name string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// ownerOf is trust.OwnerOf of the file at p.
func ownerOf(t *testing.T, p string) string {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	owner, err := trust.OwnerOf(f)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

// What it costs when this fails: a test that must see a program refuse, or leave alone, a file of
// another account's is handed one this account owns or may write, so the refusal it checks is
// never made; or it is handed a file the system's own account may write and skips where a file
// would pass, under a namespace's uid 0.
func TestSystemFileTakesTheFirstCandidateThatPassesWhoeverRunsTheTest(t *testing.T) {
	t.Parallel()
	a, b := tempFile(t, "a"), tempFile(t, "b")
	gone := filepath.Join(filepath.Dir(a), "gone")
	for _, row := range []struct {
		name  string
		cands []string
		me    string
		want  string
	}{
		{"both pass", []string{a, b}, other, a},
		{"the first not there", []string{gone, b}, other, b},
		{"as root", []string{a, b}, "uid:0", a},
		{"as SYSTEM", []string{gone, b}, "S-1-5-18", b},
	} {
		var path, owner string
		r := call(t, func(r *recorder) { path, owner = trusttest.SystemFileAs(r, row.cands, row.me, refused) })
		if r.failed || r.skipped {
			t.Errorf("%s: failed %v, skipped %v: %q, and want %s", row.name, r.failed, r.skipped, r.said, row.want)
			continue
		}
		if path != row.want || owner != ownerOf(t, row.want) {
			t.Errorf("%s: %s owned by %s, and want %s owned by %s", row.name, path, owner, row.want, ownerOf(t, row.want))
		}
	}
}

// What it costs when this fails: a test is handed, as a file this account may not write, one it
// owns, may write, cannot open, or that is no file, and its refusal is never checked; or it is
// failed with nothing said of why each candidate did not pass.
func TestSystemFileFailsNamingEachCandidateAndWhyItDidNotPass(t *testing.T) {
	t.Parallel()
	me, err := trust.Account()
	if me == "" {
		t.Fatal(err)
	}
	mine, writable, odd := tempFile(t, "mine"), tempFile(t, "writable"), tempFile(t, "odd")
	folder, gone := filepath.Dir(mine), filepath.Join(filepath.Dir(mine), "gone")
	for _, row := range []struct {
		name string
		path string
		me   string
		want string
	}{
		{"not there", gone, other, gone + ": "},
		{"a folder", folder, other, folder + ": it is not a regular file"},
		{"this account's", mine, me, mine + ": this account, " + me + ", owns it"},
		{"written", writable, other, writable + ": this account, " + other + ", opens it to write, though "},
		{"an open that fails otherwise", odd, other, odd + ": an open of it to write failed with the disk is gone, not a refusal of this account"},
	} {
		write := func(p string) error {
			switch p {
			case writable:
				return nil
			case odd:
				return errors.New("the disk is gone")
			}
			return fs.ErrPermission
		}
		r := call(t, func(r *recorder) { trusttest.SystemFileAs(r, []string{row.path}, row.me, write) })
		if !r.failed || r.skipped || !r.says(row.want) || !r.says("this account, "+row.me+", may not write") {
			t.Errorf("%s: failed %v, skipped %v: %q, and want it failed saying %q", row.name, r.failed, r.skipped, r.said, row.want)
		}
	}
	r := call(t, func(r *recorder) { trusttest.SystemFileAs(r, []string{gone, folder}, other, refused) })
	if !r.failed || !r.says(gone+": ") || !r.says("; "+folder+": it is not a regular file") {
		t.Errorf("two candidates: failed %v: %q, and want each named with why", r.failed, r.said)
	}
}

// What it costs when this fails: a run as root or SYSTEM, where no file of another account's is
// kept from it, fails a test that cannot be proved there, or one in Administrators, or any other
// account, skips one that must be, and a "not proved" passes unseen.
func TestSystemFileSkipsOnlyForTheSystemsOwnAccountWhereNoCandidatePasses(t *testing.T) {
	t.Parallel()
	gone := filepath.Join(t.TempDir(), "gone")
	for _, row := range []struct {
		me   string
		skip bool
	}{
		{"uid:0", true},
		{"S-1-5-18", true},
		{"S-1-5-32-544", false},
		{"uid:1000", false},
		{"S-1-5-21-1-2-3-500", false},
	} {
		r := call(t, func(r *recorder) { trusttest.SystemFileAs(r, []string{gone}, row.me, refused) })
		if r.skipped != row.skip || r.failed == row.skip || !r.says(row.me) || !r.says(gone+": ") {
			t.Errorf("as %s: skipped %v, failed %v: %q, and want skipped %v, naming the account and the candidate", row.me, r.skipped, r.failed, r.said, row.skip)
		}
		if row.skip && !r.says("the system's own, which may write each, so none is proved") {
			t.Errorf("as %s: %q, and want the skip to say nothing is proved", row.me, r.said)
		}
	}
}

// What it costs when this fails: on a system the library runs on, a test is handed a file of its
// own, one it may write, or one owned by another account than the one answered, so a program's
// refusal of another account's file is never checked; or no file is found at all.
func TestSystemFileAnswersAFileOfAnotherAccountsThisAccountMayNotWrite(t *testing.T) {
	t.Parallel()
	path, owner := trusttest.SystemFile(t)
	systemFileHolds(t, path, owner, wantSystemOwner(t))
}

// systemFileHolds fails t unless the file at path is owned by owner, which is want and not this
// account, and this account's open of it to write is refused.
func systemFileHolds(t *testing.T, path, owner, want string) {
	t.Helper()
	me, err := trust.Account()
	if me == "" {
		t.Fatal(err)
	}
	if got := ownerOf(t, path); got != owner || owner != want || owner == me {
		t.Errorf("%s: owned by %s by OwnerOf, answered %s, and want %s, not this account, %s", path, got, owner, want, me)
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		f.Close()
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("%s: an open of it to write: %v, and want a refusal of this account", path, err)
	}
}

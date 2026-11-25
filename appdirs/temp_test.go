//go:build unix || windows

package appdirs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/trust"
)

// absent fails t unless nothing is at p, so a refusal made no folder.
func absent(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s is there (%v), and the refusal should have made nothing", p, err)
	}
}

// folder makes the folder p.
func folder(t *testing.T, p string) string {
	t.Helper()
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

// What it costs when this fails: a program's temp folder is not its own, or not the same one at
// each run, or is answered through a link another account may point elsewhere.
func TestTempIsThisAccountsOwnFolderWithNoLinkMadeOnceAndAnsweredAgain(t *testing.T) {
	t.Parallel()
	got, err := Temp("go-files-temp-check")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != tempName("go-files-temp-check") {
		t.Errorf("Temp = %s, want a folder named %s", got, tempName("go-files-temp-check"))
	}
	if real, err := filepath.EvalSymlinks(got); err != nil || real != got {
		t.Errorf("Temp = %s, which reads as %s (%v): a link is left in it", got, real, err)
	}
	if _, err := trust.Path(got, trust.Folder, trust.Who{NoLinks: true}); err != nil {
		t.Errorf("trust refuses Temp's own answer %s: %v", got, err)
	}
	again, err := Temp("go-files-temp-check")
	if err != nil || again != got {
		t.Errorf("Temp again = %s, %v, want %s", again, err, got)
	}
	if _, err := Temp("../go-files-temp-check"); !errors.Is(err, ErrApp) {
		t.Errorf("Temp(../go-files-temp-check) = %v, want ErrApp", err)
	}
}

// What it costs when this fails: a program's temp folder is made in, or is, a tree it is kept
// apart from, as the work tree, where a head can commit a file the program then reads.
func TestATempFolderThatIsOrLiesBelowAnApartFolderIsRefusedAndNotMade(t *testing.T) {
	t.Parallel()
	b := testDir(t)
	link(t, filepath.Join(b, "lb"), b)
	for _, apart := range []string{b, filepath.Join(b, "lb")} {
		dir := filepath.Join(b, "app")
		if got, err := temp(dir, []string{apart}, nil, false); !errors.Is(err, trust.ErrInside) {
			t.Errorf("temp(%s) apart from %s = %q, %v, want ErrInside", dir, apart, got, err)
		}
		absent(t, dir)
	}
	dir := folder(t, filepath.Join(b, "made"))
	if got, err := temp(dir, []string{dir}, nil, false); !errors.Is(err, trust.ErrInside) {
		t.Errorf("temp(%s) apart from itself = %q, %v, want ErrInside", dir, got, err)
	}
}

// What it costs when this fails: a program that hands Temp its apart folders in more than one Who,
// as one for the work tree and one for its mounts, has all but the first ignored, and its temp
// folder is made below a tree it is kept apart from.
func TestTempJoinsTheApartFoldersOfEachWhoItIsGiven(t *testing.T) {
	t.Parallel()
	base, err := realPath(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const app = "go-files-temp-apart-check"
	if got, err := Temp(app, trust.Who{Apart: []string{testDir(t)}}, trust.Who{Apart: []string{base}}); !errors.Is(err, trust.ErrInside) {
		t.Errorf("Temp apart from %s in its second Who = %q, %v, want ErrInside", base, got, err)
	}
	absent(t, filepath.Join(base, tempName(app)))
}

// What it costs when this fails: a program's temp folder holds the tree it is kept apart from, so
// clearing the temp folder deletes that tree.
func TestATempFolderThatHoldsAnApartFolderIsRefusedHoweverTheFolderIsSpelled(t *testing.T) {
	t.Parallel()
	b := testDir(t)
	dir := folder(t, filepath.Join(b, "app"))
	work := folder(t, filepath.Join(dir, "work"))
	lw := filepath.Join(b, "lw")
	link(t, lw, work)
	for _, apart := range []string{work, lw} {
		if got, err := temp(dir, []string{apart}, nil, false); !errors.Is(err, trust.ErrInside) {
			t.Errorf("temp(%s) apart from %s = %q, %v, want ErrInside", dir, apart, got, err)
		}
	}
	beside := folder(t, filepath.Join(b, "beside"))
	if got, err := temp(dir, []string{beside}, nil, false); err != nil || got != dir {
		t.Errorf("temp(%s) apart from %s = %q, %v, want it", dir, beside, got, err)
	}
}

// What it costs when this fails: a folder the caller keeps the temp folder apart from, missing or
// misspelled, lets the temp folder be made before the refusal, or never refuses at all.
func TestAnApartFolderThatIsNotThereIsAnErrorBeforeAnythingIsMade(t *testing.T) {
	t.Parallel()
	b := testDir(t)
	dir := filepath.Join(b, "app")
	if got, err := temp(dir, []string{filepath.Join(b, "missing")}, nil, false); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("temp apart from a folder that is not there = %q, %v, want fs.ErrNotExist", got, err)
	}
	absent(t, dir)
	if got, err := temp(dir, []string{"rel"}, nil, false); err == nil {
		t.Errorf("temp apart from a relative folder = %q, want a refusal", got)
	}
	absent(t, dir)
}

// What it costs when this fails: a walk that ended above the temp folder's own name, or at a folder
// of apart, is taken for the pass at the name, and a folder is made that no walk before the make
// judged.
func TestANameNotThereAboveTheTempFolderIsAnErrorAndNothingIsMade(t *testing.T) {
	t.Parallel()
	b := testDir(t)
	p := filepath.Join(b, "p")
	dir := filepath.Join(p, "app")
	got, err := temp(dir, nil, func() { folder(t, p) }, false)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("temp below a folder not there = %q, %v, want fs.ErrNotExist", got, err)
	}
	absent(t, dir)
}

// What it costs when this fails: another account that may point a link on the way to the temp
// folder chooses where the program writes.
func TestALinkOnTheWayOrAtTheNameIsRefusedAndNothingIsMadeThroughIt(t *testing.T) {
	t.Parallel()
	b := testDir(t)
	real := folder(t, filepath.Join(b, "real"))
	link(t, filepath.Join(b, "l"), real)
	if got, err := temp(filepath.Join(b, "l", "app"), nil, nil, false); !errors.Is(err, trust.ErrNotPlain) {
		t.Errorf("temp through a link = %q, %v, want ErrNotPlain", got, err)
	}
	absent(t, filepath.Join(real, "app"))
	link(t, filepath.Join(b, "app"), real)
	if got, err := temp(filepath.Join(b, "app"), nil, nil, false); !errors.Is(err, trust.ErrNotPlain) {
		t.Errorf("temp at a link = %q, %v, want ErrNotPlain", got, err)
	}
}

// What it costs when this fails: another account swaps the temp folder, or a folder above it, for
// a link once the way is checked and before the folder is made, and the program writes where the
// link points.
func TestAFolderOfTheWaySwappedForALinkBetweenTheCheckAndTheMakeIsRefused(t *testing.T) {
	t.Parallel()
	b := testDir(t)
	elsewhere := folder(t, filepath.Join(b, "elsewhere"))
	dir := filepath.Join(b, "app")
	got, err := temp(dir, nil, func() { link(t, dir, elsewhere) }, false)
	if !errors.Is(err, trust.ErrNotPlain) {
		t.Errorf("temp with its name swapped for a link = %q, %v, want ErrNotPlain", got, err)
	}
	p := folder(t, filepath.Join(b, "p"))
	dir = filepath.Join(p, "app")
	got, err = temp(dir, nil, func() {
		if err := os.Rename(p, filepath.Join(b, "p-was")); err != nil {
			t.Fatal(err)
		}
		link(t, p, elsewhere)
	}, false)
	if !errors.Is(err, trust.ErrNotPlain) {
		t.Errorf("temp with its folder swapped for a link = %q, %v, want ErrNotPlain", got, err)
	}
}

// What it costs when this fails: a temp folder reached through a link or junction of this
// account's own, as a temp folder moved to another volume, is refused, or answered with the link
// left in it.
func TestATempFolderThroughALinkTrustPassesIsAnsweredAsWhatTheLinkNames(t *testing.T) {
	t.Parallel()
	b := testDir(t)
	real := folder(t, filepath.Join(b, "real"))
	want, err := realPath(real)
	if err != nil {
		t.Fatal(err)
	}
	l := filepath.Join(b, "l")
	link(t, l, real)
	for _, tmp := range []string{l, l + string(filepath.Separator)} {
		if base, err := tempBase(tmp, "app", trust.Who{}); err != nil || base != want {
			t.Errorf("tempBase(%s) = %q, %v, want %s", tmp, base, err, want)
		}
	}
	absent(t, filepath.Join(real, "app"))
	if got, err := temp(filepath.Join(want, "app"), nil, nil, false); err != nil || got != filepath.Join(want, "app") {
		t.Errorf("temp below %s = %q, %v, want %s", want, got, err, filepath.Join(want, "app"))
	}
}

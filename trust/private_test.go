package trust

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// sameAs fails t unless root is the folder at p.
func sameAs(t *testing.T, root *os.Root, p string) {
	t.Helper()
	have, err := root.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	named, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(have, named) {
		t.Errorf("the root answered is another folder than %s", p)
	}
}

// missing fails t unless nothing is at p.
func missing(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s is there (%v), and want nothing made", p, err)
	}
}

// What it costs when this fails: a program's state folder is made with the umask's mode or the
// folder above's inherited list, which its group or every account reads, or a call answers a root
// that is not the folder named.
func TestMkdirPrivateMakesEachMissingFolderPrivateAndAnswersTheLastAsARoot(t *testing.T) {
	t.Parallel()
	base := judgedDir(t)
	who := Who{NoLinks: true}
	dir := filepath.Join(base, "a", "b", "c")
	r, err := MkdirPrivate(dir, who)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	sameAs(t, r, dir)
	for _, p := range []string{filepath.Join(base, "a"), filepath.Join(base, "a", "b"), dir} {
		if _, err := Path(p, PrivateFolder, who); err != nil {
			t.Errorf("a folder MkdirPrivate made is not private: %v", err)
		}
	}
	again, err := MkdirPrivate(dir, who)
	if err != nil {
		t.Fatalf("MkdirPrivate of the private folder it made: %v", err)
	}
	defer again.Close()
	sameAs(t, again, dir)
}

// What it costs when this fails: a folder that was there and that others read is taken as private,
// or is changed under the person who set its mode or list, where the refusal and its Fix are theirs.
func TestMkdirPrivateRefusesAFolderThatWasThereAndIsNotPrivateAndLeavesItAsItWas(t *testing.T) {
	t.Parallel()
	base := judgedDir(t)
	who := Who{NoLinks: true}
	open := filepath.Join(base, "open")
	if err := os.Mkdir(open, 0o700); err != nil {
		t.Fatal(err)
	}
	openToOthers(t, open)
	_, want := Path(open, PrivateFolder, who)
	if !errors.Is(want, ErrOthersRead) {
		t.Fatalf("the folder others read is judged %v, and want ErrOthersRead, so no case here says anything", want)
	}
	_, err := MkdirPrivate(open, who)
	if u, ok := errors.AsType[*Unsafe](err); !ok || !errors.Is(err, ErrOthersRead) || u.Fix == (Fix{}) || u.Path != open {
		t.Errorf("MkdirPrivate of a folder others read = %v, and want Path's refusal of %s, ErrOthersRead with its Fix", err, open)
	}
	if _, after := Path(open, PrivateFolder, who); after == nil || after.Error() != want.Error() {
		t.Errorf("the folder others read is judged %v after MkdirPrivate, and want %v: it was changed", after, want)
	}
}

// What it costs when this fails: a folder opened to others after its path was judged is answered
// as private, as the verdict was of the name and not of the folder held.
func TestAFolderOpenedToOthersAfterItsJudgeIsRefusedOnItsHandle(t *testing.T) {
	t.Parallel()
	base := judgedDir(t)
	dir := filepath.Join(base, "a")
	opened := false
	r, err := mkdirPrivate(dir, Who{NoLinks: true}, func(step string) {
		if step != stepDir || opened {
			return
		}
		opened = true
		openToOthers(t, dir)
	})
	if err == nil {
		r.Close()
	}
	if !opened {
		t.Fatal("MkdirPrivate never reached the step after the judge of dir")
	}
	if u, ok := errors.AsType[*Unsafe](err); !ok || !errors.Is(err, ErrOthersRead) || u.Fix != (Fix{}) {
		t.Errorf("MkdirPrivate with dir opened to others after its judge = %v, and want Held's refusal, ErrOthersRead with no Fix", err)
	}
}

// What it costs when this fails: a private folder is made below a folder another account may
// change, or through a link the caller asked never to follow, or where a link that leads nowhere
// points, so whoever controls that name chooses where the program keeps its state.
func TestMkdirPrivateMakesNothingBelowAFolderAboveItRefuses(t *testing.T) {
	t.Parallel()
	base := judgedDir(t)
	shared := filepath.Join(base, "shared")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	writableByOthers(t, shared)
	_, err := MkdirPrivate(filepath.Join(shared, "x", "y"), Who{NoLinks: true})
	if u, ok := errors.AsType[*Unsafe](err); !ok || !errors.Is(err, ErrOthersWrite) || u.Path != shared {
		t.Errorf("MkdirPrivate below a folder others write = %v, and want ErrOthersWrite naming %s", err, shared)
	}
	missing(t, filepath.Join(shared, "x"))

	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "l")
	plantFolderLink(t, link, target)
	_, err = MkdirPrivate(filepath.Join(link, "x"), Who{NoLinks: true})
	if u, ok := errors.AsType[*Unsafe](err); !ok || !errors.Is(err, ErrNotPlain) || u.Path != link {
		t.Errorf("MkdirPrivate through a link with NoLinks = %v, and want ErrNotPlain naming %s", err, link)
	}
	missing(t, filepath.Join(target, "x"))
	r, err := MkdirPrivate(filepath.Join(link, "x"), Who{})
	if err != nil {
		t.Fatalf("MkdirPrivate through a link this account owns, links followed: %v", err)
	}
	defer r.Close()
	sameAs(t, r, filepath.Join(target, "x"))

	dangling := filepath.Join(base, "dangling")
	plantFolderLink(t, dangling, filepath.Join(base, "nowhere"))
	if _, err := MkdirPrivate(filepath.Join(dangling, "x"), Who{}); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("MkdirPrivate through a link to a name not there = %v, and want the walk's refusal of that name", err)
	}
	missing(t, filepath.Join(base, "nowhere"))
	named := filepath.Join(base, "named")
	plantFolderLink(t, named, filepath.Join(base, "x"))
	if _, err := MkdirPrivate(filepath.Join(named, "x"), Who{}); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("MkdirPrivate through a link to the name it would make = %v, and want the walk's refusal of that name", err)
	}
	missing(t, filepath.Join(base, "x"))
}

// What it costs when this fails: a caller that set RootOnly gets a folder this account made, which
// RootOnly trusts nowhere else, or a relative or unclean path is made against whatever folder the
// program runs in.
func TestMkdirPrivateRefusesRootOnlyAndAPathThatIsNotAbsoluteAndClean(t *testing.T) {
	t.Parallel()
	base := judgedDir(t)
	dir := filepath.Join(base, "a")
	_, err := MkdirPrivate(dir, Who{RootOnly: true})
	wantRefused(t, "MkdirPrivate with RootOnly", err, refusal{want: ErrNotOwners, says: "RootOnly"})
	missing(t, dir)
	for _, p := range []string{"", "a", filepath.Join("a", "b"), base + string(filepath.Separator) + "a" + string(filepath.Separator) + ".." + string(filepath.Separator) + "b", base + string(filepath.Separator)} {
		_, err := MkdirPrivate(p, Who{})
		wantRefused(t, "MkdirPrivate of "+strconv.Quote(p), err, refusal{says: "is not an absolute, clean path"})
	}
	missing(t, dir)
	missing(t, filepath.Join(base, "b"))
}

// What it costs when this fails: two runs of a program that start at once each make its state
// folder, and one is refused, or answered another folder than the other, and it stops or keeps its
// state apart.
func TestMakersOfOneFolderAtOnceEachAnswerIt(t *testing.T) {
	t.Parallel()
	base := judgedDir(t)
	who := Who{NoLinks: true}
	for round := range 16 {
		dir := filepath.Join(base, strconv.Itoa(round), "a", "b", "c")
		start := make(chan struct{})
		var wg sync.WaitGroup
		roots, errs := make([]*os.Root, 8), make([]error, 8)
		for i := range 8 {
			wg.Go(func() {
				<-start
				roots[i], errs[i] = MkdirPrivate(dir, who)
			})
		}
		close(start)
		wg.Wait()
		for i := range 8 {
			if errs[i] != nil {
				t.Errorf("round %d, maker %d: %v", round, i, errs[i])
				continue
			}
			sameAs(t, roots[i], dir)
			if err := roots[i].Close(); err != nil {
				t.Error(err)
			}
		}
	}
}

// What it costs when this fails: a folder another run made while this one looked is refused, so
// the second run of a program stops, or the call never gives up and spins while a process keeps
// making the folders it walks.
func TestAFolderAnotherMakeMadeWhileItWasLookedForIsJudgedAgainForEightTriesAtMost(t *testing.T) {
	t.Parallel()
	who := Who{NoLinks: true}
	for _, c := range []struct {
		makes int
		want  error
	}{{3, nil}, {privateTries, ErrReplaced}} {
		base := judgedDir(t)
		levels := make([]string, privateTries+1)
		for i := range levels {
			levels[i] = strconv.Itoa(i)
		}
		dir := filepath.Join(append([]string{base}, levels...)...)
		made := 0
		r, err := mkdirPrivate(dir, who, func(step string) {
			if step != stepFound || made == c.makes {
				return
			}
			made++
			if err := os.Mkdir(filepath.Join(append([]string{base}, levels[:made]...)...), 0o700); err != nil {
				t.Error(err)
			}
		})
		if made != c.makes {
			t.Errorf("another make made %d folders, and want %d", made, c.makes)
		}
		if c.want == nil {
			if err != nil {
				t.Errorf("MkdirPrivate cut short %d times: %v", c.makes, err)
				continue
			}
			sameAs(t, r, dir)
			if err := r.Close(); err != nil {
				t.Error(err)
			}
			continue
		}
		wantRefused(t, fmt.Sprintf("MkdirPrivate cut short at each of %d tries", c.makes), err, refusal{want: c.want, says: "at each of 8 tries"})
		missing(t, dir)
	}
}

// What it costs when this fails: a folder taken away before the make, or one another make filled,
// as Windows answers for a moment, stops the program, or a refusal that says the path leads
// elsewhere is tried again until it passes.
func TestAMakeAnotherMakeCutShortIsTriedAgainAndAnyOtherRefusalIsAnswered(t *testing.T) {
	t.Parallel()
	base := judgedDir(t)
	who := Who{NoLinks: true}
	top := filepath.Join(base, "p")
	if err := os.Mkdir(top, 0o700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(top, "q", "r")
	looks, gone := 0, false
	r, err := mkdirPrivate(dir, who, func(step string) {
		switch {
		case step == stepFound:
			looks++
		case step == stepJudged && !gone:
			gone = true
			if err := os.Remove(top); err != nil {
				t.Error(err)
			}
		}
	})
	if err != nil {
		t.Fatalf("MkdirPrivate with the folder it would make below taken away: %v", err)
	}
	defer r.Close()
	sameAs(t, r, dir)
	if looks != 2 {
		t.Errorf("MkdirPrivate looked for the nearest part %d times, and want 2", looks)
	}

	for _, c := range []struct {
		err  error
		want bool
	}{
		{fmt.Errorf("making below %s: %w", top, confine.ErrNotEmpty), true},
		{&fs.PathError{Op: "open", Path: top, Err: fs.ErrNotExist}, true},
		{fmt.Errorf("making below %s: %w", top, confine.ErrOutside), false},
		{fmt.Errorf("making below %s: %w", top, confine.ErrChanged), false},
		{fmt.Errorf("making below %s: %w", top, fs.ErrPermission), false},
	} {
		if got := raced(c.err); got != c.want {
			t.Errorf("raced(%v) = %v, and want %v", c.err, got, c.want)
		}
	}
}

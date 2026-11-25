//go:build linux || darwin || windows

package confine_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// opensRaced is how many opens race the swap.
const opensRaced = 10000

// 10,000 opens of d/f.txt while another goroutine swaps the folder d with l, a link to a folder
// outside the root that holds its own f.txt, each give the inside file or a refusal: never the
// outside file. On macOS a raw ENOTDIR or ELOOP counts as a refusal; on Windows, where the swap is
// two renames, a moment with no d is fs.ErrNotExist. How many of each the race sees hangs on the
// machine's speed, so no count is held to here: the same swap forced at each step of an open, in
// TestAFolderSwappedForALinkOutsideAtEachStepOfAnOpenIsRefusedOrGivesTheInsideFile, proves each
// answer. Before the race the swap is made and undone once, so a swapper that never swaps fails.
//
// What it costs when this fails: a process that swaps a folder for a link in a loop has a program
// read a file outside its folder, once in some thousands of opens.
func TestOpensRacedByAFolderSwappedWithALinkGiveTheInsideFileOrARefusal(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	in := filepath.Join(base, "root")
	for _, d := range []string{filepath.Join(in, "d"), filepath.Join(base, "out")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for p, body := range map[string]string{filepath.Join(in, "d", "f.txt"): "inside", filepath.Join(base, "out", "f.txt"): "outside"} {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	plantOut(t, filepath.Join(base, "out"), filepath.Join(in, "l"))
	root, err := os.OpenRoot(in)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	swap := swapper(in)
	swapsBothWays(t, in, swap)
	var stop atomic.Bool
	var flips atomic.Int64
	var wg sync.WaitGroup
	wg.Go(func() {
		for !stop.Load() {
			if swap() == nil {
				flips.Add(1)
			}
		}
	})
	inside, refusals, gaps, wrong := 0, 0, 0, 0
	for range opensRaced {
		f, err := confine.Open(root, filepath.Join("d", "f.txt"), os.O_RDONLY, 0)
		switch {
		case err == nil:
			got, err := io.ReadAll(f)
			_ = f.Close()
			if err != nil || string(got) != "inside" {
				wrong++
				if wrong <= 3 {
					t.Errorf("an open read %q, %v", got, err)
				}
				continue
			}
			inside++
		case errors.Is(err, confine.ErrOutside) || refusedHere(err):
			refusals++
		case gapHere(err):
			gaps++
		default:
			wrong++
			if wrong <= 3 {
				t.Errorf("an open failed with %v, which is neither the inside file nor a refusal", err)
			}
		}
	}
	stop.Store(true)
	wg.Wait()
	if inside+refusals+gaps+wrong != opensRaced {
		t.Fatalf("counted %d of %d opens", inside+refusals+gaps+wrong, opensRaced)
	}
	t.Logf("of %d opens over %d swaps: %d inside, %d refused, %d with no d", opensRaced, flips.Load(), inside, refusals, gaps)
	if wrong != 0 {
		t.Errorf("of %d opens over %d swaps: %d inside, %d refused, %d with no d, %d wrong; want no wrong one",
			opensRaced, flips.Load(), inside, refusals, gaps, wrong)
	}
}

// 10,000 opens by the walk of d/f.txt to be written, while another goroutine swaps the folder d
// with l, a link to the folder g inside the root, each give d's file or a refusal, and each write
// one byte to what they are given: g/f.txt, behind the link, keeps every byte. os.Root follows a
// link that stays inside, so only the walk stands between the open and g. On Windows a moment with
// no d is fs.ErrNotExist. No count of each is held to, as it hangs on the machine's speed: the swap
// forced between two steps of the walk, in TestAFolderSwappedForAnInsideLinkBetweenTwoStepsOfTheWalkIsRefused,
// proves the refusal. Before the race the swap is made and undone once, so a swapper that never
// swaps fails.
//
// What it costs when this fails: a process that swaps state for a link to .git in a loop has a
// program's writes to state/HEAD land in .git/HEAD, once in some thousands of opens, on every
// system that walks.
func TestWalkOpensRacedByAFolderSwappedWithAnInsideLinkWriteNothingBehindIt(t *testing.T) {
	t.Parallel()
	in := t.TempDir()
	for _, d := range []string{"d", "g"} {
		if err := os.Mkdir(filepath.Join(in, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for p, body := range map[string]string{filepath.Join(in, "d", "f.txt"): "inside", filepath.Join(in, "g", "f.txt"): "behind"} {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	plantOut(t, filepath.Join(in, "g"), filepath.Join(in, "l"))
	root, err := os.OpenRoot(in)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	swap := swapper(in)
	swapsBothWays(t, in, swap)
	var stop atomic.Bool
	var flips atomic.Int64
	var wg sync.WaitGroup
	wg.Go(func() {
		for !stop.Load() {
			if swap() == nil {
				flips.Add(1)
			}
		}
	})
	opened, refusals, gaps, wrong := 0, 0, 0, 0
	for range opensRaced {
		f, err := confine.WalkOpen(root, filepath.Join("d", "f.txt"), os.O_WRONLY|os.O_APPEND, 0)
		switch {
		case err == nil:
			_, err := f.WriteString("x")
			_ = f.Close()
			if err != nil {
				wrong++
				if wrong <= 3 {
					t.Errorf("a write failed: %v", err)
				}
				continue
			}
			opened++
		case errors.Is(err, confine.ErrOutside) || refusedHere(err):
			refusals++
		case gapHere(err):
			gaps++
		default:
			wrong++
			if wrong <= 3 {
				t.Errorf("an open failed with %v, which is neither d's file nor a refusal", err)
			}
		}
	}
	stop.Store(true)
	wg.Wait()
	got, err := os.ReadFile(filepath.Join(in, "g", "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	behind := len(got) - len("behind")
	t.Logf("of %d opens over %d swaps: %d opened, %d refused, %d with no d; %d bytes behind the link",
		opensRaced, flips.Load(), opened, refusals, gaps, behind)
	if behind != 0 || wrong != 0 {
		t.Errorf("of %d opens over %d swaps: %d opened, %d refused, %d with no d, %d wrong, %d bytes behind the link; want none wrong and none behind",
			opensRaced, flips.Load(), opened, refusals, gaps, wrong, behind)
	}
}

// swapsBothWays makes the swap in's rows race with, then undoes it, before the race starts: after
// the first swap d must be the link l was, to the same target, and after the second d must be what
// it was and l the link again. A swapper that never swaps would leave every open on the inside file
// and the race row green; this check turns it red.
func swapsBothWays(t *testing.T, in string, swap func() error) {
	t.Helper()
	d, l := filepath.Join(in, "d"), filepath.Join(in, "l")
	target, err := os.Readlink(l)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(d)
	if err != nil {
		t.Fatal(err)
	}
	once := func(which string) {
		for range callsPerSwap {
			if err := swap(); err != nil {
				t.Fatalf("the %s swap before the race: %v", which, err)
			}
		}
	}
	once("first")
	if got, err := os.Readlink(d); err != nil || got != target {
		t.Fatalf("after the first swap d reads as a link to %q, %v; want a link to %q", got, err, target)
	}
	once("second")
	after, err := os.Lstat(d)
	if err != nil {
		t.Fatalf("after the second swap: %v", err)
	}
	if after.Mode().Type() != before.Mode().Type() {
		t.Fatalf("after the second swap d is of type %v; want %v, as before the first", after.Mode().Type(), before.Mode().Type())
	}
	if got, err := os.Readlink(l); err != nil || got != target {
		t.Fatalf("after the second swap l reads as a link to %q, %v; want a link to %q", got, err, target)
	}
}

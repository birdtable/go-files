//go:build linux || darwin

package confine_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// plantOut makes link a symbolic link to the folder target, by a relative path.
func plantOut(t *testing.T, target, link string) {
	t.Helper()
	rel, err := filepath.Rel(filepath.Dir(link), target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rel, link); err != nil {
		t.Fatal(err)
	}
}

// gapHere is false: the swap is one call, and d is always there.
func gapHere(error) bool { return false }

// 10,000 opens by the walk of d to be written, while another goroutine swaps the file d with l, a
// link to the file g inside the root, each give d or a refusal, and each write one byte to what
// they are given: g, behind the link, keeps every byte. os.Root follows a link that stays inside,
// and answers ELOOP when the link it met is gone again before it reads it; both are refusals. No
// count of each is held to, as it hangs on the machine's speed: the swap forced before the open, in
// TestALinkSwappedInBetweenTheWalkAndTheOpenIsRefused, proves the refusal. Before the race the swap
// is made and undone once, so a swapper that never swaps fails.
//
// What it costs when this fails: a process that swaps a file for a link to .git/HEAD in a loop
// has a program's writes land in .git/HEAD, or fail with an error that is not a refusal, once in
// some thousands of opens.
func TestWalkOpensRacedByAFileSwappedWithAnInsideLinkWriteNothingBehindIt(t *testing.T) {
	t.Parallel()
	in := t.TempDir()
	for name, body := range map[string]string{"d": "inside", "g": "behind"} {
		if err := os.WriteFile(filepath.Join(in, name), []byte(body), 0o644); err != nil {
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
	opened, refusals, wrong := 0, 0, 0
	for range opensRaced {
		f, err := confine.WalkOpen(root, "d", os.O_WRONLY|os.O_APPEND, 0)
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
		case errors.Is(err, confine.ErrOutside):
			refusals++
		default:
			wrong++
			if wrong <= 3 {
				t.Errorf("an open failed with %v, which is neither d nor a refusal", err)
			}
		}
	}
	stop.Store(true)
	wg.Wait()
	got, err := os.ReadFile(filepath.Join(in, "g"))
	if err != nil {
		t.Fatal(err)
	}
	behind := len(got) - len("behind")
	t.Logf("of %d opens over %d swaps: %d opened, %d refused; %d bytes behind the link", opensRaced, flips.Load(), opened, refusals, behind)
	if behind != 0 || wrong != 0 {
		t.Errorf("of %d opens over %d swaps: %d opened, %d refused, %d wrong, %d bytes behind the link; want none wrong and none behind",
			opensRaced, flips.Load(), opened, refusals, wrong, behind)
	}
}

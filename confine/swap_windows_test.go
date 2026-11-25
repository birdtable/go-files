package confine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// plantOut makes link a junction to the folder target.
func plantOut(t *testing.T, target, link string) {
	t.Helper()
	plantJunction(t, target, link)
}

// callsPerSwap is how many calls of swapper's func trade d and l: d aside and l to d, then d to l
// and d back.
const callsPerSwap = 2

// swapper swaps in's d and l in four renames, one each call: d aside, l to d, d to l, and d back. A
// rename that fails, as one does while an open holds the folder, is tried again at the next call.
func swapper(in string) func() error {
	d, l, aside := filepath.Join(in, "d"), filepath.Join(in, "l"), filepath.Join(in, "d-aside")
	steps := [][2]string{{d, aside}, {l, d}, {d, l}, {aside, d}}
	at := 0
	return func() error {
		if err := os.Rename(steps[at][0], steps[at][1]); err != nil {
			return err
		}
		at = (at + 1) % len(steps)
		return nil
	}
}

// refusedHere is false: Windows's answers all come as ErrOutside.
func refusedHere(error) bool { return false }

// gapHere is whether err is fs.ErrNotExist, from a moment of the four renames with no d.
func gapHere(err error) bool { return errors.Is(err, fs.ErrNotExist) }

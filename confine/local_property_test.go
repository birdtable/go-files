package confine_test

import (
	"math/rand"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"

	"github.com/birdtable/go-files/confine"
)

// pieces are what the property's names are made of: the elements that climb or stay, separators of
// both systems, a drive, a stream's colon, a NUL, reserved Windows names, and plain elements.
func pieces() []string {
	return []string{"..", ".", "/", "\\", "C:", "c:", ":", "\x00", "CON", "NUL", "a", "b.txt", "...", " ", "\u2215", "//"}
}

// No name Local takes holds "..", is absolute or rooted on either system, names a drive, or holds a
// backslash, a colon or a NUL, over names made of pieces() in every order the generator reaches.
//
// What it costs when this fails: some joining of pieces the seeds never tried climbs out of the
// folder, or names a drive or another stream on Windows.
func TestNoNameLocalTakesClimbsIsRootedOrHoldsAReservedSeparator(t *testing.T) {
	t.Parallel()
	all := pieces()
	taken := 0
	held := func(picks []uint8) bool {
		var b strings.Builder
		for _, p := range picks {
			b.WriteString(all[int(p)%len(all)])
		}
		name := b.String()
		got, err := confine.Local(name)
		if err != nil {
			return true
		}
		taken++
		slash := filepath.ToSlash(got)
		for part := range strings.SplitSeq(slash, "/") {
			if part == ".." {
				return false
			}
		}
		return got == "." || (!filepath.IsAbs(got) && filepath.VolumeName(got) == "" && !strings.HasPrefix(slash, "/") &&
			!strings.ContainsAny(name, "\\:\x00"))
	}
	cfg := &quick.Config{MaxCount: 20000, Rand: rand.New(rand.NewSource(5))} //nolint:gosec // G404: a fixed seed, so a failure is the same each run
	if err := quick.Check(held, cfg); err != nil {
		t.Error(err)
	}
	if taken == 0 {
		t.Fatal("Local took no name, so the property held over nothing")
	}
}

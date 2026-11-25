package trust

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// moves is each count of reads a test answers errMoved to, the name replaced by rename between
// its reads that many times in a row, and what the call is to answer: a pass while a try is left,
// ErrReplaced once every try met a swap.
func moves() []struct {
	n    int
	want refusal
} {
	gone := refusal{ErrReplaced, Fix{}, "replaced by another file, or moved away, at each of " + strconv.Itoa(tries) + " tries"}
	return []struct {
		n    int
		want refusal
	}{{0, refusal{}}, {1, refusal{}}, {tries - 1, refusal{}}, {tries, gone}, {tries + 3, gone}}
}

// What it costs when this fails: a name its owner replaces by rename while the walk reads it, as
// write.File replaces a file, is refused as a name another account may write; or a name replaced
// at every try passes unjudged, or is refused as one of its facts, so a caller cannot tell a swap
// from a list it cannot read.
func TestANameReplacedWhileTheWalkReadsItIsReadAgainUpToItsTries(t *testing.T) {
	t.Parallel()
	p := at("f")
	for _, c := range moves() {
		name := strconv.Itoa(c.n) + " swaps"
		h := disk(map[string]node{p: file(meID, 0o600)}).host(meID)
		look, reads := h.look, 0
		h.look = func(path string) (node, error) {
			if path != p {
				return look(path)
			}
			if reads++; reads <= c.n {
				return node{}, errMoved
			}
			return look(path)
		}
		_, err := walk(p, File, Who{}, h)
		wantRefused(t, name, err, c.want)
		if errors.Is(err, ErrOthersWrite) || errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("%s: %v matches a refusal of the file's facts, none of which was judged", name, err)
		}
		if want := min(c.n+1, tries); reads != want {
			t.Errorf("%s: the name was read %d times, and want %d", name, reads, want)
		}
	}
}

// What it costs when this fails: a file its owner replaces by rename after ReadOwned opens it is
// refused as a file another account may write, or one replaced at every try is read unjudged; or
// a try leaves its descriptor open, which on Windows keeps the file from being removed.
func TestAFileReplacedAfterReadOwnedOpensItIsOpenedAgainUpToItsTries(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range moves() {
		name := strconv.Itoa(c.n) + " swaps"
		opens, opened, stats := 0, 0, 0
		h := host{root: []string{rootID}, me: meID,
			open: func(path string) (*os.File, error) {
				opens++
				return os.Open(path)
			},
			stat: func(*os.File) (node, error) {
				if stats++; stats <= c.n {
					return node{}, errMoved
				}
				n := file(meID, 0o600)
				n.size = 1
				return n, nil
			},
		}
		b, err := readOwned(p, meID, true, 10, h, func() { opened++ })
		wantRefused(t, name, err, c.want)
		switch {
		case c.want == refusal{} && string(b) != "x", c.want != refusal{} && b != nil:
			t.Errorf("%s: read %q", name, b)
		case errors.Is(err, ErrOthersWrite) || errors.Is(err, errors.ErrUnsupported):
			t.Errorf("%s: %v matches a refusal of the file's facts, none of which was judged", name, err)
		}
		if want := min(c.n+1, tries); opens != want || opened != want {
			t.Errorf("%s: the file was opened %d times and handed to the seam %d, and want %d", name, opens, opened, want)
		}
	}
}

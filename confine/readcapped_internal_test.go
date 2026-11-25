package confine

import (
	"errors"
	"io"
	"testing"
)

// zeros hands out size zero bytes, then io.EOF, and counts what it handed out.
type zeros struct{ size, given int64 }

func (z *zeros) Read(p []byte) (int, error) {
	n := min(int64(len(p)), z.size-z.given)
	if n == 0 {
		return 0, io.EOF
	}
	clear(p[:n])
	z.given += n
	return int(n), nil
}

// What it costs when this fails: a file far longer than the cap, the size of a disk, is read whole
// into memory before it is refused, when one byte past the cap is enough to tell.
func TestReadCappedReadsNoFurtherThanOneBytePastTheCap(t *testing.T) {
	t.Parallel()
	for _, limit := range []int64{0, 1, 4096, 1 << 20} {
		z := &zeros{size: 64 << 20}
		got, err := readUpTo(z, "big", limit)
		if got != nil || !errors.Is(err, ErrOverCap) || z.given > limit+1 {
			t.Errorf("a reader of %d bytes capped at %d: %d bytes answered, %v, %d read, and want none, ErrOverCap and at most %d read", z.size, limit, len(got), err, z.given, limit+1)
		}
	}
}

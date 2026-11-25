//go:build linux || darwin || freebsd

package appdirs

import (
	"math"
	"testing"
)

// room counts no room for a block count below zero, as FreeBSD gives once root has used its
// reserve, or for a block size of zero, and the most uint64 holds for a product past it.
//
// What it costs when this fails: a full FreeBSD disk reads as nearly 2^64 bytes free, so a program
// that checks for room before a large write starts it and fills the disk, or a huge file system
// reads as a few bytes and every write is refused.
func TestRoomIsNoneBelowZeroAndTheMostPastUint64(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  uint64
		want uint64
	}{
		{"blocks below zero", room(int64(-3), int64(4096)), 0},
		{"no blocks", room(uint64(0), uint32(4096)), 0},
		{"a block size of zero", room(uint64(5), uint64(0)), 0},
		{"a block size below zero", room(int64(5), int32(-1)), 0},
		{"blocks times size", room(uint64(3), uint32(4096)), 3 * 4096},
		{"past uint64", room(uint64(math.MaxUint64/2+1), int64(2)), math.MaxUint64},
		{"at uint64", room(uint64(math.MaxUint64/4), uint64(4)), math.MaxUint64 / 4 * 4},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: room = %d, want %d", c.name, c.got, c.want)
		}
	}
}

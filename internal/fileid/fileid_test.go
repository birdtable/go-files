package fileid

import "testing"

// TestKnownRejectsZeroAndAllOnes: a zero index (no file ID: a WebDAV drive, some SMB servers) and an
// all-ones index (ReFS's real ID is 128-bit and does not fit the 64 os.SameFile reads) are each "not
// known"; any other index is known.
//
// What it costs when this fails: a ReFS volume, whose index comes back all-ones, is taken as one that
// tells files apart, and os.SameFile passes a swapped file.
func TestKnownRejectsZeroAndAllOnes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		hi, lo uint32
		want   bool
	}{
		{"zero", 0, 0, false},
		{"all ones", 0xFFFFFFFF, 0xFFFFFFFF, false},
		{"low only", 0, 1, true},
		{"high only", 1, 0, true},
		{"ntfs-like", 0x140000, 0x36e994, true},
		{"high all ones, low zero", 0xFFFFFFFF, 0, true},
	}
	ran := 0
	for _, c := range cases {
		if got := Known(c.hi, c.lo); got != c.want {
			t.Errorf("Known(%#x, %#x) = %v, want %v", c.hi, c.lo, got, c.want)
		}
		ran++
	}
	if ran != len(cases) {
		t.Fatalf("ran %d of %d cases", ran, len(cases))
	}
}

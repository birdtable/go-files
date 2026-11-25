//go:build linux || darwin || freebsd

package appdirs_test

import (
	"syscall"
	"testing"

	"github.com/birdtable/go-files/appdirs"
)

// Free of the temp folder is within 1% of what the standard library's own statfs gives: blocks
// free to an account that is not root, times the block size.
//
// What it costs when this fails: Free reads the blocks free to root, which counts the reserve, or
// the wrong block size, and a program writes into a disk it was told had room.
func TestFreeIsWithinOnePercentOfStatfs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got, err := appdirs.Free(dir)
	if err != nil {
		t.Fatal(err)
	}
	var s syscall.Statfs_t
	if err := syscall.Statfs(dir, &s); err != nil {
		t.Fatal(err)
	}
	want := uint64(s.Bavail) * uint64(s.Bsize) //nolint:unconvert // each system types the two fields its own way, and both are counts
	t.Logf("Free = %d, statfs = %d, blocks free to root %d", got, want, s.Bfree)
	if diff := max(got, want) - min(got, want); diff > want/100 {
		t.Errorf("Free = %d, and statfs gives %d, %d apart, more than 1%%", got, want, diff)
	}
}

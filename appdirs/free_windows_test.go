//go:build windows

package appdirs_test

import (
	"testing"

	"github.com/birdtable/go-files/appdirs"
)

// Free of C:\ is some room. Its value is logged, for a run on Windows to hold within 1% of
// fsutil volume diskfree C:, whose "Total free bytes" counts what this account may use.
//
// What it costs when this fails: a program on Windows is told the system drive has no room, and
// refuses every large write.
func TestOnWindowsFreeOfTheSystemDriveIsSomeRoom(t *testing.T) {
	t.Parallel()
	n, err := appdirs.Free(`C:\`)
	if err != nil || n == 0 {
		t.Fatalf(`Free(C:\) = %d, %v, want some room`, n, err)
	}
	t.Logf(`Free(C:\) = %d bytes`, n)
}

package winapi

import (
	"errors"
	"io/fs"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

// A rename refused as held is tried eight times, and any other refusal once. An access list that
// denies the rename gives ERROR_ACCESS_DENIED, as a reader's hold of the old file does, and nothing
// in the error tells the two apart, so that denial is tried the eight times too, and is returned as
// it is after them: it costs about 600 ms, and widens nothing.
//
// What it costs when this fails: a rename refused for another reason waits before it is said, or
// the tries grow past their bound and a refused write hangs its caller.
func TestARenameRefusedAsHeldIsTriedEightTimesAndAnyOtherOnce(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		err   windows.Errno
		tries int
	}{
		{windows.ERROR_ACCESS_DENIED, 8},
		{windows.ERROR_SHARING_VIOLATION, 8},
		{windows.ERROR_FILE_NOT_FOUND, 1},
	} {
		n := 0
		err := Retried(func() error {
			n++
			return &os.LinkError{Op: "rename", Old: "a", New: "b", Err: c.err}
		})
		if n != c.tries || !errors.Is(err, c.err) {
			t.Errorf("a rename refused with %v: %d tries and %v, and want %d tries and the refusal as it is", c.err, n, err, c.tries)
		}
	}
}

// Dos answers an NTSTATUS as its Win32 error, which errors.Is matches to fs's errors, and passes
// any other error through as it is.
//
// What it costs when this fails: a name made at confine's no-replace target since the look is not
// fs.ErrExist, and write's retry no longer sees a held file as held.
func TestAnNTStatusIsAnsweredAsItsWin32Error(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in   error
		want error
	}{
		{windows.STATUS_OBJECT_NAME_COLLISION, fs.ErrExist},
		{windows.STATUS_OBJECT_NAME_NOT_FOUND, fs.ErrNotExist},
		{windows.STATUS_SHARING_VIOLATION, windows.ERROR_SHARING_VIOLATION},
		{fs.ErrClosed, fs.ErrClosed},
	} {
		if got := Dos(c.in); !errors.Is(got, c.want) {
			t.Errorf("Dos(%v) = %v, and want it to match %v", c.in, got, c.want)
		}
	}
	if Dos(nil) != nil {
		t.Error("Dos(nil) is not nil")
	}
}

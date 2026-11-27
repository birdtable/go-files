package loghold

import (
	"errors"
	"io/fs"
	"os"

	"golang.org/x/sys/windows"

	"github.com/birdtable/go-files/confine"
)

const noWait = 0 // an open of a name never waits for a writer here

// Far is the byte the hold locks: past the end of any log, so a reader is never refused a range.
func Far() *windows.Overlapped { return &windows.Overlapped{Offset: 0, OffsetHigh: 0x7fffffff} }

func tryHold(f *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, Far())
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}

func letGo(f *os.File) error { return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, Far()) }

// names counts by confine.NamesFile, from the handle, the one count.
func names(f *os.File, _ fs.FileInfo) (int, error) {
	return confine.NamesFile(f)
}

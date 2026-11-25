package fileid

import (
	"os"
	"sync/atomic"
	"syscall"
	"testing"
)

// seam, set by SeamNoIndex alone, makes Indexed read every volume as giving a file no index, which no
// test machine's volume does, so a package's test reaches the ErrNoFileIndex refusal.
var seam atomic.Bool //nolint:gochecknoglobals // the one test seam these packages share, toggled by SeamNoIndex under testing.Testing() alone

// Indexed reports whether f sits on a volume that gives a file a 64-bit index os.SameFile can tell it
// apart from another by. It reads the index through syscall.GetFileInformationByHandle and judges it
// by Known. forceNoIndex (a caller's own test option) or the shared seam makes it read the volume as
// one that gives a file no index; both are heeded only under testing.Testing(). An index read that
// fails is the error.
func Indexed(f *os.File, forceNoIndex bool) (bool, error) {
	var d syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &d); err != nil {
		return false, err
	}
	hi, lo := d.FileIndexHigh, d.FileIndexLow
	if (forceNoIndex || seam.Load()) && testing.Testing() {
		hi, lo = 0, 0
	}
	return Known(hi, lo), nil
}

// SeamNoIndex makes Indexed read every volume as giving a file no index until the returned func runs,
// so a package's test can reach the ErrNoFileIndex refusal on a normal volume. Indexed heeds the seam
// only under testing.Testing(), so this has no effect outside a test; a test that calls it is serial,
// as it moves shared state.
func SeamNoIndex() func() {
	seam.Store(true)
	return func() { seam.Store(false) }
}

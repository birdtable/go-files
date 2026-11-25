//go:build unix

package treesum

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// What it costs when this fails: a file swapped for a FIFO between its look and its open holds
// the walk on the open until some process writes to it, which none may.
func TestAFileSwappedForAFIFOIsRefusedWithNoWait(t *testing.T) {
	t.Parallel()
	err := swapAt(t, stepFile, "f", func(dir string) error {
		f := filepath.Join(dir, "f")
		return errors.Join(os.Remove(f), syscall.Mkfifo(f, 0o644))
	})
	if !errors.Is(err, confine.ErrOutside) {
		t.Fatalf("Sum gave %v, and want the FIFO refused", err)
	}
}

// What it costs when this fails: a folder swapped for a FIFO between its look and its open holds
// the walk on the open until some process writes to it.
func TestAFolderSwappedForAFIFOIsRefusedWithNoWait(t *testing.T) {
	t.Parallel()
	err := swapAt(t, stepFolder, "d", func(dir string) error {
		d := filepath.Join(dir, "d")
		return errors.Join(os.Remove(d), syscall.Mkfifo(d, 0o644))
	})
	if !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("Sum gave %v, and want the FIFO refused as no folder", err)
	}
}

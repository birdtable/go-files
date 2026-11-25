package treesum

import (
	"fmt"
	"os"

	"github.com/birdtable/go-files/confine"
	"github.com/birdtable/go-files/internal/fileid"
)

// indexAt refuses the folder sub opens, at the slash path at, on a Windows volume that gives it no
// 64-bit index os.SameFile can tell apart from another (fileid.ErrNoFileIndex, one value with
// write.ErrNoFileIndex, so errors.Is holds with it). It opens the folder through confine, following no
// link, for its index alone, so a tree on a WebDAV or SMB drive or on ReFS is refused rather than its
// os.SameFile check between the look and the open trusted.
func indexAt(sub *os.Root, at string) error {
	d, err := confine.Open(sub, ".", os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer d.Close() //nolint:errcheck // read only; the file index was already read
	switch ok, ierr := fileid.Indexed(d, false); {
	case ierr != nil:
		return ierr
	case !ok:
		return fmt.Errorf("%s: %w", at, fileid.ErrNoFileIndex)
	}
	return nil
}

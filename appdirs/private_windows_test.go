package appdirs

import (
	"errors"
	"path/filepath"
	"testing"
)

// On a volume that keeps no access lists, Temp's folder is refused with ErrNoAccessList, which
// matches errors.ErrUnsupported, and nothing is made.
//
// What it costs when this fails: a program's temp folder on a FAT or exFAT volume is every
// account's, while the program believes it its own.
func TestATempFolderOnAVolumeWithNoAccessListsIsRefusedAndNotMade(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(testDir(t), tempName("app"))
	got, err := temp(dir, nil, nil, true)
	if !errors.Is(err, ErrNoAccessList) || !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("temp on a volume with no access lists = %q, %v, and want ErrNoAccessList matching errors.ErrUnsupported", got, err)
	}
	absent(t, dir)
}

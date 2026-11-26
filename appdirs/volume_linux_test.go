package appdirs_test

import (
	"os"
	"testing"

	"github.com/birdtable/go-files/appdirs"
)

// On Linux a folder on procfs is Unknown, named proc by the mount table, and /dev/shm, where the
// runner has it, Local, named tmpfs.
//
// What it costs when this fails: a file system Volume does not know is answered Local, or Type is
// not the mount table's name for the mount that holds the path.
func TestVolumeAnswersUnknownOnProcAndNamesTheMountTablesType(t *testing.T) {
	t.Parallel()
	if got, err := appdirs.Volume("/proc"); err != nil || got != (appdirs.VolumeInfo{Type: "proc", Kind: appdirs.Unknown}) {
		t.Errorf("Volume(/proc) = %+v, %v, want Unknown proc and no error", got, err)
	}
	if info, err := os.Stat("/dev/shm"); err != nil || !info.IsDir() {
		t.Logf("no /dev/shm on this runner: %v", err)
		return
	}
	if got, err := appdirs.Volume("/dev/shm"); err != nil || got != (appdirs.VolumeInfo{Type: "tmpfs", Kind: appdirs.Local}) {
		t.Errorf("Volume(/dev/shm) = %+v, %v, want Local tmpfs and no error", got, err)
	}
}

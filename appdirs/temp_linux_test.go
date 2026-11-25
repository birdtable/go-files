package appdirs

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/birdtable/go-files/trust"
	"golang.org/x/sys/unix"
)

// What it costs when this fails: a program whose temp folder is on a disk mounted nosuid, as a CIS
// /tmp, /var/tmp or /home partition is, gets ErrMount from Temp with no way to name the mount. procfs stands in for that disk: every
// Linux mounts it nosuid, and a program names its type by FileSystems, as one names erofs's.
func TestOnLinuxATempFolderOnAMountNosuidIsRefusedUntilTempNamesIt(t *testing.T) {
	t.Setenv("TMPDIR", "/proc/self") // serial: the process environment, which os.TempDir reads
	procfs := trust.Who{FileSystems: []uint32{unix.PROC_SUPER_MAGIC}}
	if _, err := Temp("go-files-temp-check"); !errors.Is(err, trust.ErrMount) || !strings.Contains(err.Error(), "of type 0x9fa0") {
		t.Errorf("Temp on procfs: %v, and want ErrMount for its type", err)
	}
	if _, err := Temp("go-files-temp-check", procfs); !errors.Is(err, trust.ErrMount) || !strings.Contains(err.Error(), "mounted nosuid") {
		t.Errorf("Temp on procfs, its type named: %v, and want ErrMount for nosuid", err)
	}
	// Named, the walks pass, and the make is what refuses: procfs takes no folder.
	var pe *fs.PathError
	got, err := Temp("go-files-temp-check", procfs, trust.Who{Mounts: []string{"/proc"}})
	if errors.Is(err, trust.ErrMount) || !errors.As(err, &pe) || pe.Op != "mkdir" {
		t.Errorf("Temp on procfs, its type and /proc named: %q, %v, and want the make refused, not the mount", got, err)
	}
}

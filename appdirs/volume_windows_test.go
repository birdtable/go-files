package appdirs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/appdirs"
	"github.com/birdtable/go-files/confine"
)

// On Windows the reparse tag Volume reads is the name's own, never followed: a junction's is
// IO_REPARSE_TAG_MOUNT_POINT and the temp folder's none, and Volume answers the temp folder
// through the junction Local; the temp folder reached by the administrative share, where the
// runner can reach it, is Network, though its server's volume is the local NTFS one. It logs
// whether the share case ran, and CI prints a passed test's log lines.
//
// What it costs when this fails: the walk reads the tag of what a junction points to, or of
// nothing, so a Cloud Files placeholder is never seen; or a share of this machine's own disk is
// answered Local.
func TestVolumeReadsTheNamesOwnReparseTagAndAnswersAShareNetwork(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	links := t.TempDir()
	root, err := os.OpenRoot(links)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	if err := confine.LinkFolder(root, dir, "junction"); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(links, "junction")
	if tag, err := appdirs.ReparseTag(dir); err != nil || tag != 0 {
		t.Errorf("ReparseTag(%s) = %#x, %v, want none", dir, tag, err)
	}
	if tag, err := appdirs.ReparseTag(junction); err != nil || tag != 0xa0000003 {
		t.Errorf("ReparseTag(%s) = %#x, %v, want IO_REPARSE_TAG_MOUNT_POINT", junction, tag, err)
	}
	if got, err := appdirs.Volume(junction); err != nil || got.Kind != appdirs.Local {
		t.Errorf("Volume(%s) = %+v, %v, want Local", junction, got, err)
	}
	vol := filepath.VolumeName(dir)
	if len(vol) != 2 {
		t.Logf("the share case did not run: the temp folder %s is on no drive letter", dir)
		return
	}
	share := `\\localhost\` + vol[:1] + `$` + dir[2:]
	if _, err := os.Stat(share); err != nil {
		t.Logf("the share case did not run: no administrative share on this runner: %v", err)
		return
	}
	got, err := appdirs.Volume(share)
	if err != nil || got.Kind != appdirs.Network {
		t.Errorf("Volume(%s) = %+v, %v, want Network", share, got, err)
	}
	t.Logf("the share case ran: Volume(%s) = %+v, %v", share, got, err)
}

package appdirs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/appdirs"
	"golang.org/x/sys/unix"
)

// On macOS a folder on devfs, which carries the local-mount flag and is neither APFS, HFS+, FAT nor
// exFAT, is Unknown, named devfs, with no error.
//
// What it costs when this fails: the local-mount flag alone decides, as it would for a macFUSE
// volume mounted with -o local.
func TestVolumeAnswersUnknownOnDevfs(t *testing.T) {
	t.Parallel()
	if got, err := appdirs.Volume("/dev"); err != nil || got != (appdirs.VolumeInfo{Type: "devfs", Kind: appdirs.Unknown}) {
		t.Errorf("Volume(/dev) = %+v, %v, want Unknown devfs and no error", got, err)
	}
}

// On macOS a folder below one that carries a File Provider attribute is SyncPlaceholder, and a
// folder below an unmarked Library/CloudStorage folder Unknown, both on the temp folder's APFS
// volume, which Volume answers Local beside them.
//
// What it costs when this fails: a folder in a File Provider domain, iCloud Drive or a third-party
// one, is answered by its APFS volume alone, so Local.
func TestVolumeReadsTheFileProvidersMarksOnAnAPFSVolume(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marked := filepath.Join(dir, "domain", "data")
	unmarked := filepath.Join(dir, "Library", "CloudStorage", "Provider", "data")
	for _, d := range []string{marked, unmarked} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Setxattr(filepath.Join(dir, "domain"), "com.apple.fileprovider.dir#N", []byte{'1'}, 0); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		path string
		want appdirs.VolumeKind
	}{
		{dir, appdirs.Local},
		{marked, appdirs.SyncPlaceholder},
		{unmarked, appdirs.Unknown},
	} {
		if got, err := appdirs.Volume(c.path); err != nil || got.Kind != c.want || got.Type != "apfs" {
			t.Errorf("Volume(%s) = %+v, %v, want %v on apfs", c.path, got, err, c.want)
		}
	}
}

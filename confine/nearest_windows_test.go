package confine_test

import (
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// What it costs when this fails: on Windows the walk passes a share's root to the server's, or
// stops short of a drive's root, so a marker there is missed or one outside the share is asked
// for over the network.
func TestOnWindowsNearestEndsAtADrivesRootAndAShares(t *testing.T) {
	t.Parallel()
	var looked []string
	record := func(p string) (fs.FileInfo, error) { looked = append(looked, p); return nil, fs.ErrNotExist }
	share := `\\host\share\a\b`
	_, err := confine.NearestLooked(share, "mark", "", record)
	want := []string{`\\host\share\a\b\mark`, `\\host\share\a\mark`, `\\host\share\mark`}
	if !errors.Is(err, fs.ErrNotExist) || !slices.Equal(looked, want) {
		t.Errorf("from %s: asked %q, %v, want %q", share, looked, err, want)
	}
	looked = nil
	if _, err := confine.NearestLooked(share, "mark", `\\host\share\`, record); !errors.Is(err, fs.ErrNotExist) || len(looked) != 3 {
		t.Errorf("top at the share's root: asked %q, %v, want its three folders", looked, err)
	}
	looked = nil
	for _, top := range []string{`\\host\other\`, `\\HOST\SHARE\`} {
		if _, err := confine.NearestLooked(share, "mark", top, record); !errors.Is(err, confine.ErrOutside) || len(looked) != 0 {
			t.Errorf("top %s, not spelled as a folder of the walk: asked %q, %v, want ErrOutside before any look", top, looked, err)
		}
	}

	start := t.TempDir()
	drive := filepath.VolumeName(start) + `\`
	looked = nil
	if _, err := confine.NearestLooked(start, "mark", "", record); !errors.Is(err, fs.ErrNotExist) || len(looked) < 2 || looked[len(looked)-1] != drive+"mark" {
		t.Errorf("from %s: asked %q, %v, want the last at %s", start, looked, err, drive+"mark")
	}
	put(t, filepath.Join(start, "mark"))
	if got, err := confine.Nearest(filepath.Join(start, "x"), "mark", drive); err != nil || got != start {
		t.Errorf("top at %s: %q, %v, want %q", drive, got, err, start)
	}
}

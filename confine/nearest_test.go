package confine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/birdtable/go-files/confine"
)

// nearestTree makes tmp/a/b/c and answers tmp and c's path.
func nearestTree(t *testing.T) (tmp, start string) {
	t.Helper()
	tmp = t.TempDir()
	start = filepath.Join(tmp, "a", "b", "c")
	if err := os.MkdirAll(start, 0o700); err != nil {
		t.Fatal(err)
	}
	return tmp, start
}

// put writes an empty file at each of paths, making its folders.
func put(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// What it costs when this fails: a program takes the wrong folder for its project's root, a
// farther one or none, and confines its reads and writes there.
func TestNearestAnswersTheNearestFolderThatHoldsTheMarker(t *testing.T) {
	t.Parallel()
	tmp, start := nearestTree(t)
	a, b := filepath.Join(tmp, "a"), filepath.Join(tmp, "a", "b")
	put(t, filepath.Join(start, "here"), filepath.Join(a, "two-up"), filepath.Join(tmp, "at-top"),
		filepath.Join(b, ".app", "pin.yaml"), filepath.Join(b, "near"), filepath.Join(tmp, "near"),
		filepath.Join(start, "f"), filepath.Join(a, "f", "x"))
	rows := []struct{ marker, top, want, why string }{
		{"here", "", start, "the marker at the start"},
		{"two-up", "", a, "the marker two folders up"},
		{"at-top", tmp, tmp, "the marker at the top"},
		{".app/pin.yaml", "", b, "a marker of two names"},
		{"near", "", b, "a nearer marker, over a farther one"},
		{"f/x", "", a, "past a folder whose marker's inner name is a file"},
		{"two-up", filepath.Join(tmp, "a", "..", "a"), a, "a top written unclean"},
	}
	for _, r := range rows {
		if got, err := confine.Nearest(start, r.marker, r.top); err != nil || got != r.want {
			t.Errorf("%s: Nearest(%q, %q, %q) = %q, %v, want %q", r.why, start, r.marker, r.top, got, err, r.want)
		}
	}
	got, err := confine.Nearest(start, "nowhere", tmp)
	var pe *fs.PathError
	if !errors.Is(err, fs.ErrNotExist) || !errors.As(err, &pe) || pe.Path != "nowhere" || got != "" {
		t.Errorf("no marker at or below the top: %q, %v, want a PathError naming the marker, matching fs.ErrNotExist", got, err)
	}
	if got, err := confine.Nearest(filepath.Join(start, "x", "..", "..", "c"), "two-up", ""); err != nil || got != a {
		t.Errorf("a start written unclean: %q, %v, want %q", got, err, a)
	}
}

// What it costs when this fails: a marker a repository commits as a link, or one whose target is
// gone, is passed over, and a folder farther up is taken for the project's root.
func TestNearestCountsAMarkerThatIsALinkAndOneThatDangles(t *testing.T) {
	t.Parallel()
	tmp, start := nearestTree(t)
	put(t, filepath.Join(tmp, "target"), filepath.Join(tmp, "link"), filepath.Join(tmp, "dangling"))
	plantLink(t, filepath.Join(tmp, "target"), filepath.Join(start, "link"))
	plantLink(t, filepath.Join(tmp, "gone"), filepath.Join(start, "dangling"))
	for _, m := range []string{"link", "dangling"} {
		if got, err := confine.Nearest(start, m, tmp); err != nil || got != start {
			t.Errorf("the marker %s at the start: %q, %v, want %q", m, got, err, start)
		}
	}
}

// What it costs when this fails: the walk passes the folder the caller trusts as its ceiling and
// takes a marker another account planted above it.
func TestNearestStopsAtTopAndRefusesOneNotAboveTheStart(t *testing.T) {
	t.Parallel()
	tmp, start := nearestTree(t)
	b := filepath.Join(tmp, "a", "b")
	put(t, filepath.Join(tmp, "a", "mark"), filepath.Join(tmp, "other", "mark"))
	for _, top := range []string{start, b} {
		if got, err := confine.Nearest(start, "mark", top); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("top at %s, below the marker: %q, %v, want fs.ErrNotExist", top, got, err)
		}
	}
	for _, top := range []string{filepath.Join(tmp, "other"), filepath.Join(start, "below")} {
		looked := 0
		got, err := confine.NearestLooked(start, "mark", top, func(p string) (fs.FileInfo, error) { looked++; return os.Lstat(p) })
		if !errors.Is(err, confine.ErrOutside) || got != "" || looked != 0 {
			t.Errorf("top %s, not the start or above it: %q, %v, after %d looks, want ErrOutside before any look", top, got, err, looked)
		}
	}
}

// What it costs when this fails: a marker from a file or a flag climbs out (../x), or names a
// folder by its absolute path, and the walk answers a folder that holds it.
func TestNearestRefusesAMarkerLocalRefusesAndAnEmptyOne(t *testing.T) {
	t.Parallel()
	_, start := nearestTree(t)
	checked := 0
	for _, m := range append(refusedPaths(), "") {
		looked := 0
		got, err := confine.NearestLooked(start, m, "", func(p string) (fs.FileInfo, error) { looked++; return os.Lstat(p) })
		if !errors.Is(err, confine.ErrOutside) || got != "" || looked != 0 {
			t.Errorf("the marker %q: %q, %v, after %d looks, want ErrOutside before any look", m, got, err, looked)
		}
		checked++
	}
	if checked < 2 {
		t.Fatalf("checked %d markers", checked)
	}
}

// What it costs when this fails: a folder whose look fails for want of access is taken for one
// with no marker, and the walk answers a folder farther up.
func TestNearestStopsAtALookThatFailsOtherwiseThanNotThere(t *testing.T) {
	t.Parallel()
	tmp, start := nearestTree(t)
	b := filepath.Join(tmp, "a", "b")
	put(t, filepath.Join(tmp, "a", "mark"))
	denied := filepath.Join(b, "mark")
	got, err := confine.NearestLooked(start, "mark", "", func(p string) (fs.FileInfo, error) {
		if p == denied {
			return nil, &fs.PathError{Op: "lstat", Path: p, Err: fs.ErrPermission}
		}
		return os.Lstat(p)
	})
	var pe *fs.PathError
	if !errors.Is(err, fs.ErrPermission) || !errors.As(err, &pe) || pe.Path != denied || got != "" {
		t.Errorf("a denied look at %s, below the marker: %q, %v, want that error with its path", denied, got, err)
	}
}

// What it costs when this fails: the walk ends before the top of the volume, or asks a folder
// twice, or out of order.
func TestNearestAsksEachFolderUpToTheVolumesTopInOrder(t *testing.T) {
	t.Parallel()
	_, start := nearestTree(t)
	var want []string
	for d := start; ; d = filepath.Dir(d) {
		want = append(want, filepath.Join(d, "mark"))
		if filepath.Dir(d) == d {
			break
		}
	}
	var looked []string
	_, err := confine.NearestLooked(start, "mark", "", func(p string) (fs.FileInfo, error) {
		looked = append(looked, p)
		return nil, fs.ErrNotExist
	})
	if !errors.Is(err, fs.ErrNotExist) || !slices.Equal(looked, want) {
		t.Errorf("the walk asked %q, %v, want %q", looked, err, want)
	}
	if len(want) < 4 {
		t.Fatalf("the walk from %s has %d folders", start, len(want))
	}
}

// typed is an fs.FileInfo of one name with the mode given, for a look a test answers itself.
type typed struct {
	name string
	mode fs.FileMode
}

func (f typed) Name() string       { return f.name }
func (f typed) Size() int64        { return 0 }
func (f typed) Mode() fs.FileMode  { return f.mode }
func (f typed) ModTime() time.Time { return time.Time{} }
func (f typed) IsDir() bool        { return f.mode.IsDir() }
func (f typed) Sys() any           { return nil }

// A marker that is a pipe, a device, a character device or a socket is no file or folder: Nearest
// refuses it where the look finds it, matching fs.ErrInvalid and naming it, and asks no folder
// above. A file, a folder and a link at the marker are each answered as the folder that holds it.
//
// What it costs when this fails: a pipe or a device another account put where a marker is looked
// for is taken for the marker, and its folder for the project's root.
func TestNearestRefusesAMarkerThatIsAPipeADeviceOrASocket(t *testing.T) {
	t.Parallel()
	tmp, start := nearestTree(t)
	b := filepath.Join(tmp, "a", "b")
	at := filepath.Join(b, "mark")
	for _, c := range []struct {
		mode    fs.FileMode
		refused bool
	}{
		{fs.ModeNamedPipe, true},
		{fs.ModeDevice, true},
		{fs.ModeDevice | fs.ModeCharDevice, true},
		{fs.ModeSocket, true},
		{0, false},
		{fs.ModeDir, false},
		{fs.ModeSymlink, false},
	} {
		var asked []string
		got, err := confine.NearestLooked(start, "mark", "", func(p string) (fs.FileInfo, error) {
			asked = append(asked, p)
			if p == at {
				return typed{"mark", c.mode}, nil
			}
			return nil, &fs.PathError{Op: "lstat", Path: p, Err: fs.ErrNotExist}
		})
		var pe *fs.PathError
		switch {
		case !c.refused && (err != nil || got != b):
			t.Errorf("a marker of mode %v at %s: %q, %v, and want %s", c.mode, at, got, err, b)
		case c.refused && (!errors.Is(err, fs.ErrInvalid) || !errors.As(err, &pe) || pe.Path != at || got != ""):
			t.Errorf("a marker of mode %v at %s: %q, %v, and want a refusal matching fs.ErrInvalid naming it", c.mode, at, got, err)
		case c.refused && slices.Contains(asked, filepath.Join(tmp, "a", "mark")):
			t.Errorf("a marker of mode %v at %s: the walk asked a folder above it, %v", c.mode, at, asked)
		}
	}
}

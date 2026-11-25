package trusttest_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/birdtable/go-files/trust"
	"github.com/birdtable/go-files/trust/trusttest"
)

// recorder is a testing.TB that keeps what a helper says, and the cleanups it asks for, in place
// of failing, skipping or ending the test; a fatal or a skip ends only the call.
type recorder struct {
	testing.TB
	failed, skipped bool
	said            []string
	cleanups        []func()
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.failed = true
	r.said = append(r.said, fmt.Sprintf(format, args...))
}

func (r *recorder) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	runtime.Goexit()
}

func (r *recorder) Skipf(format string, args ...any) {
	r.skipped = true
	r.said = append(r.said, fmt.Sprintf(format, args...))
	runtime.Goexit()
}

func (r *recorder) Cleanup(f func()) { r.cleanups = append(r.cleanups, f) }

// end runs the cleanups, the last asked for first, as a test's end does.
func (r *recorder) end() {
	for _, f := range slices.Backward(r.cleanups) {
		f()
	}
	r.cleanups = nil
}

// says is whether the recorder said, failing or skipping, anything holding s.
func (r *recorder) says(s string) bool { return strings.Contains(strings.Join(r.said, "\n"), s) }

// call is f called with a recorder of t, once it has returned or ended.
func call(t *testing.T, f func(r *recorder)) *recorder {
	t.Helper()
	r := &recorder{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		f(r)
	}()
	<-done
	return r
}

// private is a file, mode 0600, or a folder, mode 0700, of the test's own, which Perm holds
// private to this account before the test changes it.
func private(t *testing.T, folder bool) (string, fs.FileMode) {
	t.Helper()
	p, mode := filepath.Join(t.TempDir(), "name"), fs.FileMode(0o600)
	var err error
	if folder {
		mode = 0o700
		err = os.Mkdir(p, mode)
	} else {
		err = os.WriteFile(p, []byte("data"), mode)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil { // past the umask
		t.Fatal(err)
	}
	trusttest.Perm(t, p, mode)
	return p, mode
}

// held is trust.Held's answer for the name at p, of the kind last, for no Who.
func held(t *testing.T, p string, last trust.Kind) error {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	return trust.Held(f, last, trust.Who{})
}

// kinds is the private kind and the plain kind trust judges p by, a folder's or a file's.
func kinds(folder bool) (private, plain trust.Kind) {
	if folder {
		return trust.PrivateFolder, trust.Folder
	}
	return trust.Key, trust.File
}

// What it costs when this fails: a test that lets another account read or write a name to see a
// program refuse it lets in no one, so the refusal it checks is never made, or the name stays
// open after the test, to the next test and to every account.
func TestLoosenLetsWhoInSoTrustRefusesTheNameUntilTheTestEnds(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		who    trusttest.Who
		writes bool
		bit    fs.FileMode
	}{
		{"its group, to read", trusttest.GroupReads, false, 0o040},
		{"its group, to write", trusttest.GroupWrites, true, 0o020},
		{"others, to read", trusttest.OthersRead, false, 0o004},
		{"others, to write", trusttest.OthersWrite, true, 0o002},
	} {
		for _, folder := range []bool{false, true} {
			p, mode := private(t, folder)
			key, plain := kinds(folder)
			r := call(t, func(r *recorder) { trusttest.Loosen(r, p, c.who) })
			if r.failed || r.skipped {
				t.Fatalf("%s, a folder %v: %q", c.name, folder, r.said)
			}
			if _, ok := errors.AsType[*trust.Unsafe](held(t, p, key)); !ok {
				t.Errorf("%s, a folder %v: trust passes it as private, let in", c.name, folder)
			}
			if _, ok := errors.AsType[*trust.Unsafe](held(t, p, plain)); ok != c.writes {
				t.Errorf("%s, a folder %v: trust refuses it, let in, as any kind: %v, and want %v", c.name, folder, ok, c.writes)
			}
			// The mode bit on Unix; on Windows the mode os.Chmod gives is the one it had.
			if m := call(t, func(m *recorder) { trusttest.Perm(m, p, mode|c.bit) }); m.failed {
				t.Errorf("%s, a folder %v: %q", c.name, folder, m.said)
			}
			r.end()
			if r.failed {
				t.Errorf("%s, a folder %v: putting it back: %q", c.name, folder, r.said)
			}
			trusttest.Perm(t, p, mode)
		}
	}
}

// What it costs when this fails: a test of a name this account cannot read reads it after all,
// so the refusal it checks is never made, or the name stays unreadable, and its folder cannot be
// removed, after the test.
func TestDenyKeepsThisAccountOutUntilTheTestEnds(t *testing.T) {
	t.Parallel()
	for _, folder := range []bool{false, true} {
		p, mode := private(t, folder)
		r := call(t, func(r *recorder) { trusttest.Deny(r, p) })
		if os.Geteuid() == 0 {
			if !r.skipped || r.failed || len(r.cleanups) != 0 {
				t.Errorf("as root, a folder %v: skipped %v, failed %v, %d cleanups, %q, and want a skip with no change left", folder, r.skipped, r.failed, len(r.cleanups), r.said)
			}
			trusttest.Perm(t, p, mode)
			continue
		}
		if r.failed || r.skipped {
			t.Fatalf("a folder %v: %q", folder, r.said)
		}
		if f, err := os.Open(p); !errors.Is(err, fs.ErrPermission) {
			if err == nil {
				_ = f.Close() // only opened
			}
			t.Errorf("a folder %v: this account opens it, denied, with %v", folder, err)
		}
		r.end()
		if r.failed {
			t.Errorf("a folder %v: putting it back: %q", folder, r.said)
		}
		f, err := os.Open(p)
		if err != nil {
			t.Fatalf("a folder %v: put back, and not opened: %v", folder, err)
		}
		_ = f.Close() // only opened
		trusttest.Perm(t, p, mode)
	}
}

// What it costs when this fails: an open of the denied name that fails for a reason of its own,
// as a name removed meanwhile or a volume gone, skips the test as though this account opened it
// still, so a test that should fail is never seen red; or the name stays denied after.
func TestDenyFailsAnOpenThatFailsForAReasonOtherThanTheDenial(t *testing.T) {
	t.Parallel()
	for _, folder := range []bool{false, true} {
		p, mode := private(t, folder)
		gone := func(name string) (*os.File, error) {
			return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
		}
		r := call(t, func(r *recorder) { trusttest.DenyBy(r, p, gone) })
		if !r.failed || r.skipped || len(r.said) != 1 || !r.says("not a refusal of this account") || len(r.cleanups) != 0 {
			t.Errorf("a folder %v, its open failing with fs.ErrNotExist: failed %v, skipped %v, %d cleanups, %q, and want one failure, no skip, nothing left changed", folder, r.failed, r.skipped, len(r.cleanups), r.said)
		}
		trusttest.Perm(t, p, mode)

		other := filepath.Join(t.TempDir(), "other")
		if err := os.WriteFile(other, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		opens := func(string) (*os.File, error) { return os.Open(other) }
		r = call(t, func(r *recorder) { trusttest.DenyBy(r, p, opens) })
		if !r.skipped || r.failed || !r.says("opens it still") || len(r.cleanups) != 0 {
			t.Errorf("a folder %v, its open opening: skipped %v, failed %v, %d cleanups, %q, and want a skip, closed, nothing left changed", folder, r.skipped, r.failed, len(r.cleanups), r.said)
		}
		trusttest.Perm(t, p, mode)
	}
}

// What it costs when this fails: a test that asks for a mode, or for a name private to this
// account, passes a name that is not, or one that asks for what no system keeps passes.
func TestPermFailsAModeOtherThanTheOneKeptAndANameNotPrivate(t *testing.T) {
	t.Parallel()
	t.Run("a mode other than its own", func(t *testing.T) {
		t.Parallel()
		p, _ := private(t, false)
		for _, c := range []struct {
			name string
			want fs.FileMode
			said string
		}{
			{"its owner write taken away", 0o400, "its mode is"},
			{"bits beyond the nine", 0o1600, "beyond the nine"},
			{"setuid", fs.ModeSetuid | 0o600, "beyond the nine"},
		} {
			if r := call(t, func(r *recorder) { trusttest.Perm(r, p, c.want) }); !r.failed || !r.says(c.said) {
				t.Errorf("%s: failed %v, %q, and want a failure saying %q", c.name, r.failed, r.said, c.said)
			}
		}
		if r := call(t, func(r *recorder) { trusttest.Perm(r, filepath.Join(filepath.Dir(p), "none"), 0o600) }); !r.failed {
			t.Errorf("a name not there passes")
		}
	})
	t.Run("not private", func(t *testing.T) {
		t.Parallel()
		p, _ := private(t, false)
		trusttest.Loosen(t, p, trusttest.OthersRead)
		if r := call(t, func(r *recorder) { trusttest.Perm(r, p, 0o600) }); !r.failed || !r.says("not private") {
			t.Errorf("a name others may read passes as private: %q", r.said)
		}
		if r := call(t, func(r *recorder) { trusttest.Perm(r, p, 0o604) }); r.failed {
			t.Errorf("a name others may read, asked for as such: %q", r.said)
		}
	})
	t.Run("no owner read", func(t *testing.T) {
		t.Parallel()
		p, _ := private(t, false)
		if err := os.Chmod(p, 0o200); err != nil {
			t.Fatal(err)
		}
		r := call(t, func(r *recorder) { trusttest.Perm(r, p, 0o200) })
		if err := os.Chmod(p, 0o600); err != nil {
			t.Fatal(err)
		}
		if !r.failed || !r.says("lets its owner not read") {
			t.Errorf("a private want with no owner read: failed %v, %q", r.failed, r.said)
		}
	})
	t.Run("no Who", func(t *testing.T) {
		t.Parallel()
		p, mode := private(t, false)
		for _, who := range []trusttest.Who{0, trusttest.OthersWrite + 1} {
			if r := call(t, func(r *recorder) { trusttest.Loosen(r, p, who) }); !r.failed || !r.says("is no Who") || len(r.cleanups) != 0 {
				t.Errorf("Loosen with Who %d: failed %v, %d cleanups, %q", who, r.failed, len(r.cleanups), r.said)
			}
		}
		trusttest.Perm(t, p, mode)
	})
}

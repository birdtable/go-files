//go:build unix

package confine_test

import (
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// The gap this module fills, held against the Go it builds with: os.Root
// alone follows a link that stays inside it, at a middle component and at
// the last, even when the caller passes O_NOFOLLOW, and a create through a
// dangling link makes the link's target. Nothing in os.Root's set says
// otherwise, so confine walks the path itself.
//
// What it costs when this fails: nothing breaks, but os.Root has learned to
// refuse such a link, and confine may become thinner.
func TestOSRootAloneFollowsALinkThatStaysInside(t *testing.T) {
	t.Parallel()
	root, dir := folder(t)
	read := func(name string, flag int) string {
		t.Helper()
		f, err := root.OpenFile(filepath.FromSlash(name), flag, 0)
		if err != nil {
			t.Fatalf("os.Root refused %s: %v", name, err)
		}
		defer f.Close()
		b, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if got := read("state/HEAD", os.O_RDONLY); got != "head\n" {
		t.Errorf("os.Root read state/HEAD as %q, want real/HEAD's", got)
	}
	if got := read("history.jsonl", os.O_RDONLY|syscall.O_NOFOLLOW); got != "config\n" {
		t.Errorf("os.Root read history.jsonl with O_NOFOLLOW as %q, want real/config's", got)
	}
	f, err := root.OpenFile("dangling", os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatalf("os.Root refused a create through a dangling link: %v", err)
	}
	_ = f.Close()
	if _, err := os.Lstat(filepath.Join(dir, "real", "gone")); err != nil {
		t.Errorf("os.Root's create through dangling did not make real/gone: %v", err)
	}
}

// A file Open makes has the mode it was asked for, narrowed by the umask as
// os.Root's own create is: confine adds no mode of its own.
//
// What it costs when this fails: a file made through Open is more open than asked, or less, and a secret
// lands readable by others.
func TestAMadeFileHasTheModeAskedFor(t *testing.T) {
	// serial: sets the process umask, which every file a parallel test made would take
	old := syscall.Umask(0o022)
	defer syscall.Umask(old)
	root, dir := plainFolder(t)
	f, err := confine.Open(root, "made", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	st, err := os.Stat(filepath.Join(dir, "made"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o640 {
		t.Errorf("the made file is %v, want 0640", st.Mode().Perm())
	}
}

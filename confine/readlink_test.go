package confine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// readlinkTree makes tmp/root/sub, opens tmp/root, and answers tmp and the root.
func readlinkTree(t *testing.T) (tmp string, root *os.Root) {
	t.Helper()
	tmp = t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "root", "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(filepath.Join(tmp, "root"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return tmp, root
}

// refusedAs checks that err is an *fs.PathError naming at, slash-separated, that matches want.
func refusedAs(t *testing.T, why, at string, target string, err, want error) {
	t.Helper()
	pe, ok := errors.AsType[*fs.PathError](err)
	if !errors.Is(err, want) || !ok || pe.Path != at || target != "" {
		t.Errorf("%s: Readlink(%q) = %q, %v, want a PathError naming %q matching %v", why, at, target, err, at, want)
	}
}

// What it costs when this fails: a program that reads a link to judge where it leads judges
// another text than the link holds, one cleaned or resolved, and passes a link out of its folder.
func TestReadlinkAnswersTheTargetAsTheLinkHoldsIt(t *testing.T) {
	t.Parallel()
	tmp, root := readlinkTree(t)
	in := filepath.Join(tmp, "root")
	rows := []struct{ name, target, why string }{
		{"abs", filepath.Join(tmp, "elsewhere", "t"), "an absolute target outside the root"},
		{"rel", filepath.FromSlash("../x/./y"), "a relative target, not cleaned, that leads up"},
		{"gone", "gone", "a target that is not there"},
		{filepath.Join("sub", "near"), filepath.FromSlash("../abs"), "a link in a folder, to a link"},
	}
	for _, r := range rows {
		plantLink(t, r.target, filepath.Join(in, r.name))
		if got, err := confine.Readlink(root, r.name); err != nil || got != r.target {
			t.Errorf("%s: Readlink(%q) = %q, %v, want %q", r.why, r.name, got, err, r.target)
		}
	}
}

// What it costs when this fails: a program that may not import syscall cannot tell a name that is
// no link, or one not there, from any other failure, and treats a file as a link it could not read.
func TestReadlinkRefusesANameThatIsNoLinkOrNotThere(t *testing.T) {
	t.Parallel()
	tmp, root := readlinkTree(t)
	put(t, filepath.Join(tmp, "root", "sub", "f"))
	for _, at := range []string{"sub/f", "sub"} {
		got, err := confine.Readlink(root, filepath.FromSlash(at))
		refusedAs(t, "no link", at, got, err, fs.ErrInvalid)
	}
	for _, at := range []string{"nothing", "sub/nothing", "no/where"} {
		got, err := confine.Readlink(root, filepath.FromSlash(at))
		refusedAs(t, "not there", at, got, err, fs.ErrNotExist)
	}
}

// What it costs when this fails: the read follows a link among the name's folders, so a name a
// program takes as below its root reads a link elsewhere, or a link it must refuse.
func TestReadlinkRefusesALinkBeforeTheLastNameTheRootAndAnUncleanName(t *testing.T) {
	t.Parallel()
	tmp, root := readlinkTree(t)
	in := filepath.Join(tmp, "root")
	plantLink(t, "t", filepath.Join(in, "sub", "x"))
	plantLink(t, "sub", filepath.Join(in, "lnk")) // relative, so os.Root would follow it
	for _, name := range []string{filepath.Join("lnk", "x"), "."} {
		if got, err := confine.Readlink(root, name); !errors.Is(err, confine.ErrOutside) || errors.Is(err, fs.ErrNotExist) || got != "" {
			t.Errorf("Readlink(%q) = %q, %v, want ErrOutside and not fs.ErrNotExist", name, got, err)
		}
	}
	sep := string(filepath.Separator)
	checked := 0
	for _, name := range []string{"..", filepath.Join("..", "x"), "sub" + sep + ".." + sep + "x", "sub" + sep + "." + sep + "x",
		filepath.Join(in, "sub", "x"), "", "sub" + sep} {
		if got, err := confine.Readlink(root, name); !errors.Is(err, confine.ErrOutside) || got != "" {
			t.Errorf("Readlink(%q) = %q, %v, want ErrOutside", name, got, err)
		}
		checked++
	}
	if checked < 2 {
		t.Fatalf("checked %d names", checked)
	}
}

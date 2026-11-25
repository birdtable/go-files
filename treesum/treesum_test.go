package treesum_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/birdtable/go-files/confine"
	"github.com/birdtable/go-files/treesum"
)

// big is a cap no test tree reaches.
func big() treesum.Caps { return treesum.Caps{Entries: 1000, Bytes: 1 << 20} }

// plant makes, beneath dir, each entry of spec: a name ending in "/" is a folder, "-> t" a link
// to t, and anything else a file holding the text, made 0o644.
func plant(t *testing.T, dir string, spec ...string) {
	t.Helper()
	for _, s := range spec {
		name, rest, _ := strings.Cut(s, " ")
		p := filepath.Join(dir, filepath.FromSlash(name))
		var err error
		switch {
		case strings.HasSuffix(name, "/"):
			err = os.MkdirAll(p, 0o755)
		case strings.HasPrefix(rest, "-> "):
			err = os.Symlink(filepath.FromSlash(strings.TrimPrefix(rest, "-> ")), p)
		default:
			err = os.WriteFile(p, []byte(rest), 0o644)
			if err == nil {
				err = os.Chmod(p, 0o644)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func rootAt(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

// tree plants spec in a new folder and opens it as a root.
func tree(t *testing.T, spec ...string) *os.Root {
	t.Helper()
	dir := t.TempDir()
	plant(t, dir, spec...)
	return rootAt(t, dir)
}

const mixedListing = "folder\ta\t\n" +
	"file\ta/f\t5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03\n" +
	"link\ta/up\t../e\n" +
	"folder\te\t\n" +
	"link\tl\ta/f\n"

// mixedDigest is the SHA-256 of mixedListing, as shasum -a 256 gives it.
const mixedDigest = "t2:44a046609b31eaa243ae812f399f36d333fce9440496e59182c7c4991e554b72"

// What it costs when this fails: a tree pinned on one system does not match itself on another,
// and every check there refuses the tree it was given, or the README's format is not the one
// the code writes.
func TestATreeWithALinkGivesTheSameDigestOnEverySystem(t *testing.T) {
	t.Parallel()
	// A folder, a file, an empty folder, and two links, one up and across, with no executable file.
	root := tree(t, "a/", "a/f hello\n", "e/", "l -> a/f", "a/up -> ../e")
	digest, listing, err := treesum.Sum(root, ".", big())
	if err != nil {
		t.Fatal(err)
	}
	if string(listing) != mixedListing {
		t.Errorf("the listing is\n%q\nand want\n%q", listing, mixedListing)
	}
	if digest != mixedDigest {
		t.Errorf("the digest is %s, and want %s", digest, mixedDigest)
	}
}

// runDigest is the digest of a tree of one file, run, holding "#!/bin/sh\n": the SHA-256 of
// "file\trun\t<hash>\n", as shasum -a 256 gives it.
const runDigest = "t2:8dfb75034f8267e201f3c025d9d0a661ac91a6fd1ae7a7d0fa2b497f30252017"

// What it costs when this fails: a tree pinned on Linux or macOS with an executable file fails
// its check on Windows, which reports no executable bit, or the other way round.
func TestAnExecutableFileDigestsAsTheSameWithTheBitClearedOnEverySystem(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plant(t, dir, "run #!/bin/sh\n")
	for _, mode := range []fs.FileMode{0o755, 0o644, 0o700} {
		if err := os.Chmod(filepath.Join(dir, "run"), mode); err != nil {
			t.Fatal(err)
		}
		if digest, _, err := treesum.Sum(rootAt(t, dir), ".", big()); err != nil || digest != runDigest {
			t.Errorf("with the mode %v the digest is %s (%v), and want %s", mode, digest, err, runDigest)
		}
	}
}

// The listing and digest of mixedListing's tree as the earlier format gave them, a
// file's line holding - after its path, as shasum -a 256 gives the digest.
const (
	t1Listing = "folder\ta\t\n" +
		"file\ta/f\t-\t5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03\n" +
		"link\ta/up\t../e\n" +
		"folder\te\t\n" +
		"link\tl\ta/f\n"
	t1Digest = "t1:e7836200a53b6ba9f12a60e0a588bf633df6fe2be1372bc8b97f170bb7269a34"
)

// What it costs when this fails: a tree pinned by the old format is refused as changed, with no
// word that it is the pin that is old, or a digest of another kind is taken for one of treesum's.
func TestCheckRefusesADigestOfAnotherFormatByName(t *testing.T) {
	t.Parallel()
	root := tree(t, "a/", "a/f hello\n", "e/", "l -> a/f", "a/up -> ../e")
	for _, c := range []struct{ digest, listing, format string }{
		{t1Digest, t1Listing, `"t1:"`},
		{"t1:" + mixedDigest[3:], mixedListing, `"t1:"`},
		{"h1:" + mixedDigest[3:], mixedListing, `"h1:"`},
		{"T2:" + mixedDigest[3:], mixedListing, `"T2:"`},
		{":" + mixedDigest[3:], mixedListing, `":"`},
	} {
		err := treesum.Check(root, ".", big(), c.digest, []byte(c.listing))
		if !errors.Is(err, treesum.ErrVersion) || err == nil || !strings.Contains(err.Error(), c.format) {
			t.Errorf("Check of %s gave %v, and want ErrVersion naming %s", c.digest, err, c.format)
		}
	}
	// The format is refused before the tree is read: a name the root does not hold, and caps
	// no tree passes, would each give their own refusal were it read first.
	for _, caps := range []treesum.Caps{big(), {Entries: -1}} {
		if err := treesum.Check(root, "missing", caps, t1Digest, []byte(t1Listing)); !errors.Is(err, treesum.ErrVersion) {
			t.Errorf("Check of a t1: digest at a name the root does not hold, with caps %+v, gave %v, and want ErrVersion before the tree is read", caps, err)
		}
	}
	if err := treesum.Check(root, ".", big(), mixedDigest, []byte(mixedListing)); err != nil {
		t.Errorf("Check of the tree's own digest refused it: %v", err)
	}
}

// refused fails t unless Sum of root refuses the entry at, with an error matching want.
func refused(t *testing.T, root *os.Root, at string, want error) {
	t.Helper()
	digest, _, err := treesum.Sum(root, ".", big())
	if !errors.Is(err, want) {
		t.Fatalf("Sum gave %q, %v, and want a refusal matching %v", digest, err, want)
	}
	if pe, ok := errors.AsType[*fs.PathError](err); !ok || pe.Path != at {
		t.Errorf("the refusal %v names no entry %s", err, at)
	}
}

// What it costs when this fails: a pinned tree holds a link out of it, and the program that runs
// from the tree reads or runs what the link names, which the pin never saw.
func TestALinkThatLeadsOutOfTheTreeIsRefused(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		spec []string
		at   string
	}{
		{"absolute", []string{"l -> /etc/passwd"}, "l"},
		{"a volume", []string{"l -> C:/Windows"}, "l"},
		{"a UNC share", []string{"l -> //server/share"}, "l"},
		{"climbs out through ..", []string{"d/", "d/l -> ../../x"}, "d/l"},
		{"climbs out through another link", []string{"sub/", "sub/up -> ..", "l -> sub/up/.."}, "l"},
		{"climbs out as Windows reads it, by its text", []string{"x/y/z/", "d -> x/y/z", "l -> d/../.."}, "l"},
		{"climbs after a missing name", []string{"l -> missing/../x"}, "l"},
		{"climbs after a file", []string{"f a", "l -> f/../x"}, "l"},
		{"climbs after a link to a missing name", []string{"m -> gone", "l -> m/../x"}, "l"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			refused(t, tree(t, c.spec...), c.at, confine.ErrOutside)
		})
	}
}

// What it costs when this fails: a loop of links in a pinned tree hangs the walk or is hashed,
// and a program that follows it runs on the system's refusal.
func TestALoopOfLinksIsRefused(t *testing.T) {
	t.Parallel()
	refused(t, tree(t, "a -> b", "b -> a"), "a", confine.ErrOutside)
	refused(t, tree(t, "self -> self/x"), "self", confine.ErrOutside)
}

// What it costs when this fails: links that stay inside, the ones a browser folder holds, are
// refused, and no such tree can be pinned.
func TestALinkThatStaysInsideIsHashedAsItsText(t *testing.T) {
	t.Parallel()
	root := tree(t, "v/a/", "v/a/lib x", "v/cur -> a", "lib -> v/cur/lib", "dangling -> not/there", "dot -> .")
	_, listing, err := treesum.Sum(root, ".", big())
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"link\tlib\tv/cur/lib\n", "link\tv/cur\ta\n", "link\tdangling\tnot/there\n", "link\tdot\t.\n"} {
		if !strings.Contains(string(listing), line) {
			t.Errorf("the listing holds no %q:\n%s", line, listing)
		}
	}
}

// What it costs when this fails: a file of the tree has a second name outside it, which a
// program changes, and the pinned tree changes with no entry of it touched.
func TestAFileWithASecondNameIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plant(t, dir, "t/", "t/f a")
	if err := os.Link(filepath.Join(dir, "t", "f"), filepath.Join(dir, "other")); err != nil {
		t.Fatal(err)
	}
	digest, _, err := treesum.Sum(rootAt(t, dir), "t", big())
	if !errors.Is(err, confine.ErrOutside) {
		t.Fatalf("Sum gave %q, %v, and want the hard link refused", digest, err)
	}
}

// What it costs when this fails: a hostile tree, one of millions of entries or of a file without
// end, makes the walk run as long as it likes.
func TestEachCapIsARefusal(t *testing.T) {
	t.Parallel()
	root := tree(t, "a/", "a/f 0123456789", "b/")
	if _, _, err := treesum.Sum(root, ".", treesum.Caps{Entries: 3, Bytes: 10}); err != nil {
		t.Fatalf("a tree at its caps is refused: %v", err)
	}
	for _, caps := range []treesum.Caps{{Entries: 2, Bytes: 10}, {Entries: 3, Bytes: 9}, {Entries: -1, Bytes: 10}} {
		if _, _, err := treesum.Sum(root, ".", caps); !errors.Is(err, treesum.ErrCap) {
			t.Errorf("with caps %+v Sum gave %v, and want ErrCap", caps, err)
		}
	}
	// A folder, its file and a file are three entries, the top folder not counted.
	three := tree(t, "a/", "a/f x", "g y")
	if _, _, err := treesum.Sum(three, ".", treesum.Caps{Entries: 3, Bytes: 2}); err != nil {
		t.Errorf("with Entries 3 Sum of a folder, its file and a file gave %v, and want it passed", err)
	}
	if _, _, err := treesum.Sum(three, ".", treesum.Caps{Entries: 2, Bytes: 2}); !errors.Is(err, treesum.ErrCap) {
		t.Errorf("with Entries 2 Sum of a folder, its file and a file gave %v, and want ErrCap", err)
	}
	// A cap below zero is refused before the walk, also where the tree never spends it.
	if _, _, err := treesum.Sum(tree(t, "e/"), ".", treesum.Caps{Entries: 1, Bytes: -1}); !errors.Is(err, treesum.ErrCap) {
		t.Errorf("with a byte cap of -1 Sum of a tree with no file gave %v, and want ErrCap", err)
	}
}

// What it costs when this fails: a tree pinned is taken when it changed, or the caller is told
// it changed with no word of where.
func TestCheckNamesTheFirstEntryThatDiffers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plant(t, dir, "a/", "a/f one", "b/", "b/g two")
	root := rootAt(t, dir)
	digest, listing, err := treesum.Sum(root, ".", big())
	if err != nil {
		t.Fatal(err)
	}
	if err := treesum.Check(root, ".", big(), digest, listing); err != nil {
		t.Fatalf("the tree it pinned is refused: %v", err)
	}
	if err := treesum.Check(root, ".", big(), mixedDigest, listing); !errors.Is(err, treesum.ErrDiffers) {
		t.Errorf("the tree's own listing with another digest gave %v, and want ErrDiffers", err)
	}
	for _, c := range []struct{ change, undo string }{{"a/f changed", "a/f one"}, {"a/e new", ""}, {"", "b/g two"}} {
		if c.change != "" {
			plant(t, dir, c.change)
		} else if err := os.Remove(filepath.Join(dir, "b", "g")); err != nil {
			t.Fatal(err)
		}
		at := pathOf(c.change + c.undo)
		err := treesum.Check(root, ".", big(), digest, listing)
		if pe, ok := errors.AsType[*fs.PathError](err); !errors.Is(err, treesum.ErrDiffers) || !ok || pe.Path != at {
			t.Errorf("with %s changed Check gave %v, and want ErrDiffers naming %s", at, err, at)
		}
		if c.undo != "" {
			plant(t, dir, c.undo)
		} else if err := os.Remove(filepath.Join(dir, "a", "e")); err != nil {
			t.Fatal(err)
		}
	}
	if err := treesum.Check(root, ".", big(), digest, listing); err != nil {
		t.Errorf("the tree put back is refused: %v", err)
	}
}

// pathOf is the name a spec line of plant makes.
func pathOf(spec string) string {
	name, _, _ := strings.Cut(spec, " ")
	return name
}

// What it costs when this fails: a tree whose top is a link, or a file, is walked from where the
// link leads.
func TestTheTopIsAFolderReachedWithNoLink(t *testing.T) {
	t.Parallel()
	root := tree(t, "real/", "top -> real", "f a")
	for _, name := range []string{"top", "f"} {
		if _, _, err := treesum.Sum(root, name, big()); err == nil {
			t.Errorf("Sum of %s took it", name)
		}
	}
	if _, _, err := treesum.Sum(root, "real", big()); err != nil {
		t.Errorf("Sum of a folder refused it: %v", err)
	}
}

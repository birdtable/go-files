package confine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// resolvedTemp is a new temporary folder, resolved, so a temp folder reached through a link, as
// macOS's /var is, is spelled as the walk answers it.
func resolvedTemp(t *testing.T) string {
	t.Helper()
	dir, err := confine.Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// tree makes, in base, a folder tree holding a file x, a folder sub, and a folder out beside it
// holding its own x and a folder sub, so a path that leads out answers another x.
func tree(t *testing.T, base string) {
	t.Helper()
	for _, d := range []string{"tree/sub", "out/sub"} {
		if err := os.MkdirAll(filepath.Join(base, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"tree/x", "out/x"} {
		if err := os.WriteFile(filepath.Join(base, filepath.FromSlash(f)), []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func at(base string, names ...string) string {
	return filepath.Join(append([]string{base}, names...)...)
}

// resolvesTo holds Resolve(path) to want.
func resolvesTo(t *testing.T, path, want string) {
	t.Helper()
	got, err := confine.Resolve(path)
	if err != nil || got != want {
		t.Errorf("Resolve(%s) = %q, %v, want %q", path, got, err, want)
	}
}

// Each link on a path is followed, a relative target against the link's own folder, an absolute
// one from its top, and a link among a target's own names too; a path with no link answers itself.
//
// What it costs when this fails: a product that asks where a name leads, to judge whether it is
// inside a tree, is answered the link's own name, as filepath.EvalSymlinks answers a Windows
// junction, and a link out of the tree is judged inside.
func TestResolveFollowsEachLinkOneHopAtATime(t *testing.T) {
	t.Parallel()
	base := resolvedTemp(t)
	tree(t, base)
	plantLink(t, at("..", "out"), at(base, "tree", "rel"))
	plantLink(t, at(base, "out"), at(base, "tree", "abs"))
	plantLink(t, "rel", at(base, "tree", "chain"))
	plantLink(t, at("chain", "sub"), at(base, "tree", "through"))
	resolvesTo(t, at(base, "tree", "x"), at(base, "tree", "x"))
	resolvesTo(t, at(base, "tree", "rel", "x"), at(base, "out", "x"))
	resolvesTo(t, at(base, "tree", "abs", "x"), at(base, "out", "x"))
	resolvesTo(t, at(base, "tree", "chain"), at(base, "out"))
	resolvesTo(t, at(base, "tree", "through"), at(base, "out", "sub"))
}

// A .. that follows a plain folder in a target goes up from it, and so stays inside; a .. that
// follows a link in a target is taken as the system takes it: on Linux and macOS from the folder
// the link leads to, so tree/l -> d/../x with d a link out answers out's x; on Windows by its
// spelling, against l's folder.
//
// What it costs when this fails: a resolver that cleans a target before it reads the links in it
// drops d before d is read, and answers tree/x for a link the kernel follows out.
func TestResolveTakesADotDotInATargetAsTheSystemDoes(t *testing.T) {
	t.Parallel()
	base := resolvedTemp(t)
	tree(t, base)
	plantLink(t, at(base, "out", "sub"), at(base, "tree", "d"))
	plantLink(t, raw("d", "..", "x"), at(base, "tree", "l"))
	plantLink(t, raw("sub", "..", "x"), at(base, "tree", "plain"))
	resolvesTo(t, at(base, "tree", "l"), at(base, filepath.FromSlash(dotDotAfterLink)))
	resolvesTo(t, at(base, "tree", "plain"), at(base, "tree", "x"))
}

// A loop of links, and a chain of more than 32, are refused with ErrLoop, which matches
// ErrOutside; a chain of 32 resolves. 32 is macOS's own limit, the lowest of the systems'.
//
// What it costs when this fails: a loop planted in a tree walks forever, or a chain is cut short
// and answered as if it ended there.
func TestResolveRefusesALoopByName(t *testing.T) {
	t.Parallel()
	base := resolvedTemp(t)
	plantLink(t, "b", at(base, "a"))
	plantLink(t, "a", at(base, "b"))
	for _, path := range []string{at(base, "a"), at(base, "a", "x")} {
		_, err := confine.Resolve(path)
		if !errors.Is(err, confine.ErrLoop) || !errors.Is(err, confine.ErrOutside) {
			t.Errorf("Resolve(%s) = %v, want ErrLoop, matching ErrOutside", path, err)
		}
	}
	if err := os.WriteFile(at(base, "end"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	prev := "end"
	for i := range 33 {
		name := "c" + strconv.Itoa(i)
		plantLink(t, prev, at(base, name))
		prev = name
	}
	resolvesTo(t, at(base, "c31"), at(base, "end"))
	if _, err := confine.Resolve(at(base, "c32")); !errors.Is(err, confine.ErrLoop) {
		t.Errorf("Resolve of a chain of 33 links = %v, want ErrLoop", err)
	}
}

// Names below the last one that exists are kept as written, so a dangling link answers where it
// leads; a .. after a missing name is refused, as the system cannot walk up from it, and a file
// walked through is ENOTDIR, by the walk alone where the system walks a .. after it.
//
// What it costs when this fails: a link to something missing outside the tree answers nothing, or
// its own name, and is judged inside; or a .. after a missing folder is taken by its spelling and
// answers a name the system would never reach.
func TestResolveKeepsMissingNamesAndRefusesAWalkThroughThem(t *testing.T) {
	t.Parallel()
	base := resolvedTemp(t)
	tree(t, base)
	plantLink(t, at(base, "out", "gone", "deeper"), at(base, "tree", "dangling"))
	resolvesTo(t, at(base, "tree", "dangling"), at(base, "out", "gone", "deeper"))
	resolvesTo(t, at(base, "tree", "dangling", "more"), at(base, "out", "gone", "deeper", "more"))
	plantLink(t, raw("gone", "..", "x"), at(base, "tree", "updown"))
	got, err := confine.Resolve(at(base, "tree", "updown"))
	if dotDotAfterMissing == "" {
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Resolve(tree/updown -> gone/../x) = %q, %v, want fs.ErrNotExist", got, err)
		}
	} else if err != nil || got != at(base, filepath.FromSlash(dotDotAfterMissing)) {
		t.Errorf("Resolve(tree/updown -> gone/../x) = %q, %v, want %s", got, err, dotDotAfterMissing)
	}
	plantLink(t, raw("x", "..", "sub"), at(base, "tree", "fx"))
	got, err = confine.ResolveWalk(at(base, "tree", "fx"))
	if dotDotAfterFile == "" {
		if !errors.Is(err, syscall.ENOTDIR) {
			t.Errorf("the walk alone of tree/fx -> x/../sub, x a file = %q, %v, want ENOTDIR", got, err)
		}
	} else if err != nil || got != at(base, filepath.FromSlash(dotDotAfterFile)) {
		t.Errorf("the walk alone of tree/fx -> x/../sub, x a file = %q, %v, want %s", got, err, dotDotAfterFile)
	}
	_, err = confine.Resolve(at(base, "tree", "x", "y"))
	if !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("Resolve(tree/x/y) = %v, want ENOTDIR", err)
	}
}

// A path that is not absolute is refused with an error matching fs.ErrInvalid and ErrOutside, by
// Resolve and by Below; Below's folder that is not absolute, or not clean, is refused too.
//
// What it costs when this fails: a relative path is taken against a working folder that moves
// under it; a folder spelled with a .. is compared as whichever folder its walk ends in, not the
// one the caller named.
func TestResolveTakesOnlyAnAbsolutePath(t *testing.T) {
	t.Parallel()
	base := resolvedTemp(t)
	for _, path := range []string{"a", filepath.Join("a", "b")} {
		_, err := confine.Resolve(path)
		if !errors.Is(err, fs.ErrInvalid) || !errors.Is(err, confine.ErrOutside) {
			t.Errorf("Resolve(%q) = %v, want fs.ErrInvalid and ErrOutside", path, err)
		}
		if _, err := confine.Below(base, path); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Below(base, %q) = %v, want fs.ErrInvalid", path, err)
		}
	}
	for _, folder := range []string{"a", filepath.Join("a", "b"), raw(base, "a", "..", "b"), raw(base, ".", "b")} {
		if _, err := confine.Below(folder, base); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Below(%q, base) = %v, want fs.ErrInvalid", folder, err)
		}
	}
}

// An absolute path that is not clean is taken where the system takes it: on Linux and macOS
// walked as written, so tree/d/../x, with d a link to out/sub, is out's x; on Windows cleaned
// first, by its spelling, so it is tree\x. A ., a doubled separator and a trailing separator after
// a folder are stepped over. The system's own look at each path as given finds the file at the
// answer, and Below names those that end in tree and refuses the one that leads out.
//
// What it costs when this fails: a caller holding a path it may not clean (a link's target, a
// value from a file) has no way to ask where it leads, and cleans it to pass the check; the clean
// drops d before d is read, and a path the kernel follows out is judged inside.
func TestResolveTakesAnUncleanPathWhereTheSystemTakesIt(t *testing.T) {
	t.Parallel()
	base := resolvedTemp(t)
	tree(t, base)
	in := at(base, "tree")
	plantLink(t, at(base, "out", "sub"), at(in, "d"))
	sep := string(filepath.Separator)
	resolvesTo(t, raw(in, "d", "..", "x"), at(base, filepath.FromSlash(dotDotAfterLink)))
	for path, below := range map[string]string{
		raw(in, "d", "..", "x"):               dotDotBelow,
		raw(in, ".", "x"):                     "x",
		in + sep + sep + "x":                  "x",
		raw(in, "d", "..", "..", "tree", "x"): "x",
		raw(in, "sub", "..", "x"):             "x",
		raw(in, "d") + sep:                    "",
	} {
		got, err := confine.Resolve(path)
		if err != nil {
			t.Errorf("Resolve(%s) = %v, want the file the system finds", path, err)
			continue
		}
		sys, sysErr := os.Stat(path)
		ans, ansErr := os.Stat(got)
		if sysErr != nil || ansErr != nil || !os.SameFile(sys, ans) {
			t.Errorf("Resolve(%s) = %q, which is not the file os.Stat finds at the path (%v, %v)", path, got, sysErr, ansErr)
		}
		name, err := confine.Below(in, path)
		switch {
		case below == "" && !errors.Is(err, confine.ErrOutside):
			t.Errorf("Below(tree, %s) = %q, %v, want ErrOutside", path, name, err)
		case below != "" && (err != nil || name != below):
			t.Errorf("Below(tree, %s) = %q, %v, want %s", path, name, err, below)
		}
	}
}

// An unclean path is refused where the system refuses it: tree/gone/../x with fs.ErrNotExist and
// tree/x/../sub, x a file, with ENOTDIR on Linux and macOS, which walk each name, while Windows takes
// both by their spelling, as tree\x and tree\sub; and tree/x/, x a file, with the system's own
// error on every system.
//
// What it costs when this fails: a path the system would never open is answered as a file, and a
// caller acts on a name it was given in a form the system refuses.
func TestResolveRefusesAnUncleanPathWhereTheSystemDoes(t *testing.T) {
	t.Parallel()
	base := resolvedTemp(t)
	tree(t, base)
	in := at(base, "tree")
	got, err := confine.Resolve(raw(in, "gone", "..", "x"))
	if dotDotAfterMissing == "" {
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Resolve(tree/gone/../x) = %q, %v, want fs.ErrNotExist", got, err)
		}
	} else if err != nil || got != at(base, filepath.FromSlash(dotDotAfterMissing)) {
		t.Errorf("Resolve(tree/gone/../x) = %q, %v, want %s", got, err, dotDotAfterMissing)
	}
	got, err = confine.Resolve(raw(in, "x", "..", "sub"))
	if dotDotAfterFile == "" {
		if !errors.Is(err, syscall.ENOTDIR) {
			t.Errorf("Resolve(tree/x/../sub), x a file = %q, %v, want ENOTDIR", got, err)
		}
	} else if err != nil || got != at(base, filepath.FromSlash(dotDotAfterFile)) {
		t.Errorf("Resolve(tree/x/../sub), x a file = %q, %v, want %s", got, err, dotDotAfterFile)
	}
	trailing := at(in, "x") + string(filepath.Separator)
	_, sysErr := os.Stat(trailing)
	var sysPath *fs.PathError
	if !errors.As(sysErr, &sysPath) {
		t.Fatalf("the system's own os.Stat(tree/x/), x a file = %v, want its refusal", sysErr)
	}
	if got, err := confine.Resolve(trailing); !errors.Is(err, sysPath.Err) {
		t.Errorf("Resolve(tree/x/), x a file = %q, %v, want the system's own %v", got, err, sysPath.Err)
	}
}

// A link swapped for another between the walk and the system's own look at the path is refused
// as changed, matching ErrOutside, and nothing is answered.
//
// What it costs when this fails: a link pointed back inside for the walk and out again at once is
// answered as inside, and a copy follows it out.
func TestResolveRefusesALinkChangedWhileItWasWalked(t *testing.T) {
	t.Parallel()
	base := resolvedTemp(t)
	tree(t, base)
	link := at(base, "tree", "l")
	plantLink(t, "sub", link)
	got, err := confine.ResolveWalked(link, func() {
		if err := os.Remove(link); err != nil {
			t.Error(err)
		}
		plantLink(t, at(base, "out", "sub"), link)
	})
	if !errors.Is(err, confine.ErrOutside) || got != "" {
		t.Errorf("Resolve with the link swapped after the walk = %q, %v, want a refusal matching ErrOutside", got, err)
	}
	gone := at(base, "tree", "m")
	plantLink(t, "sub", gone)
	_, err = confine.ResolveWalked(gone, func() {
		if err := os.Remove(gone); err != nil {
			t.Error(err)
		}
	})
	if !errors.Is(err, confine.ErrOutside) {
		t.Errorf("Resolve with the link removed after the walk = %v, want a refusal matching ErrOutside", err)
	}
	missing := at(base, "tree", "n")
	plantLink(t, "later", missing)
	_, err = confine.ResolveWalked(missing, func() {
		if err := os.Mkdir(at(base, "tree", "later"), 0o755); err != nil {
			t.Error(err)
		}
	})
	if !errors.Is(err, confine.ErrOutside) {
		t.Errorf("Resolve with the missing target made after the walk = %v, want a refusal matching ErrOutside", err)
	}
}

// Below answers a path's name in a folder, "." for the folder itself, through a link into it, and
// refuses one that leads out, a dangling one among them, with ErrOutside.
//
// What it costs when this fails: a link out of a tree is judged inside it and a copy carries the
// outside file in, or a link into the tree is refused and the copy fails.
func TestBelowAnswersTheNameInTheFolderOrRefusesIt(t *testing.T) {
	t.Parallel()
	base := resolvedTemp(t)
	tree(t, base)
	in := at(base, "tree")
	plantLink(t, at(base, "out"), at(in, "outside"))
	plantLink(t, at(base, "out", "gone"), at(in, "dangling"))
	plantLink(t, at(in, "sub"), at(base, "out", "into"))
	plantLink(t, in, at(base, "alias"))
	plantLink(t, at(base, "out", "sub"), at(in, "d"))
	plantLink(t, raw("d", "..", "x"), at(in, "l"))
	for path, want := range map[string]string{
		in:                        ".",
		at(in, "x"):               "x",
		at(in, "sub", "new", "y"): at("sub", "new", "y"),
		at(base, "out", "into"):   "sub",
		at(base, "alias", "x"):    "x",
		at(in, "l"):               dotDotBelow,
		at(in, "outside", "x"):    "",
		at(in, "dangling"):        "",
		at(base, "out"):           "",
	} {
		got, err := confine.Below(in, path)
		if want == "" {
			if !errors.Is(err, confine.ErrOutside) && !errors.Is(err, fs.ErrInvalid) {
				t.Errorf("Below(tree, %s) = %q, %v, want a refusal matching ErrOutside", path, got, err)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("Below(tree, %s) = %q, %v, want %q", path, got, err, want)
		}
	}
	if _, err := confine.Below(at(in, "x"), at(in, "x")); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("Below of a file as the folder = %v, want fs.ErrInvalid", err)
	}
}

// Below compares by file ID, not spelling: where the volume folds case, the folder spelled in
// another case is the same folder; where it does not, that spelling names nothing.
//
// What it costs when this fails: a tree spelled TREE on a folding volume refuses every file in
// it, or a product guesses folding from the system's name and judges a case-sensitive one wrong.
func TestBelowComparesTheFolderByIDNotSpelling(t *testing.T) {
	t.Parallel()
	base := resolvedTemp(t)
	tree(t, base)
	upper := at(base, "TREE")
	_, folds := os.Stat(upper)
	got, err := confine.Below(upper, at(base, "tree", "x"))
	switch {
	case folds == nil && (err != nil || got != "x"):
		t.Errorf("on a volume that folds case, Below(TREE, tree/x) = %q, %v, want x", got, err)
	case folds != nil && !errors.Is(err, fs.ErrNotExist):
		t.Errorf("on a volume that keeps case, Below(TREE, tree/x) = %q, %v, want fs.ErrNotExist", got, err)
	}
}

// raw is names joined by the separator as written, with no clean: a target with a .. kept.
func raw(names ...string) string { return strings.Join(names, string(filepath.Separator)) }

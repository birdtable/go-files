package confine

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// raced is a folder holding a.txt and b.txt, opened as a root, and the
// hook open calls at each step: it runs each change of changes once, the
// first time open reaches its step. The test fails unless every step was
// reached.
func raced(t *testing.T, changes map[string]func(dir string)) (*os.Root, string, func(string)) {
	t.Helper()
	if len(changes) == 0 {
		t.Fatal("no change to race")
	}
	dir := t.TempDir()
	for name, body := range map[string]string{"a.txt": "a\n", "b.txt": "b\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	done := map[string]bool{}
	t.Cleanup(func() {
		_ = root.Close()
		for step := range changes {
			if !done[step] {
				t.Errorf("open never reached %q, so nothing was raced there", step)
			}
		}
	})
	return root, dir, func(at string) {
		if change, ok := changes[at]; ok && !done[at] {
			done[at] = true
			change(dir)
		}
	}
}

// holds fails the test unless the file at name in dir holds want.
func holds(t *testing.T, dir, name, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil || string(got) != want {
		t.Errorf("%s holds %q, %v, want %q", name, got, err, want)
	}
}

// must fails the test on err, from a change made in a race.
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// refused fails the test unless err matches ErrOutside.
func refused(t *testing.T, what string, f *os.File, err error) {
	t.Helper()
	if f != nil {
		_ = f.Close()
	}
	if !errors.Is(err, ErrOutside) {
		t.Errorf("%s = %v, want ErrOutside", what, err)
	}
}

// A file swapped for a link between the walk and the open is refused, and
// os.O_TRUNC empties nothing: the link's target keeps every byte.
//
// What it costs when this fails: a process that swaps a link in at the
// right moment has a replace empty the file it points to.
func TestALinkSwappedInBetweenTheWalkAndTheOpenIsRefused(t *testing.T) {
	t.Parallel()
	root, dir, hook := raced(t, map[string]func(string){stepOpen: func(dir string) {
		must(t, os.Remove(filepath.Join(dir, "a.txt")))
		plantLink(t, "b.txt", filepath.Join(dir, "a.txt"))
	}})
	f, err := walkOpen(root, "a.txt", os.O_WRONLY|os.O_TRUNC, 0, hook)
	refused(t, "Open(a.txt) with a link swapped in", f, err)
	holds(t, dir, "b.txt", "b\n")
}

// A file swapped for another file between the walk and the open is refused:
// it is not the file the walk found.
//
// What it costs when this fails: a process that renames another file over the one found has a program read
// or write the wrong file.
func TestAFileSwappedInBetweenTheWalkAndTheOpenIsRefused(t *testing.T) {
	t.Parallel()
	root, _, hook := raced(t, map[string]func(string){stepOpen: func(dir string) {
		must(t, os.Rename(filepath.Join(dir, "b.txt"), filepath.Join(dir, "a.txt")))
	}})
	f, err := walkOpen(root, "a.txt", os.O_RDONLY, 0, hook)
	refused(t, "Open(a.txt) with another file renamed over it", f, err)
}

// A link made at a missing name between the walk and the create is not
// followed: the exclusive create meets it, the second walk refuses it, and
// its target is not made.
//
// What it costs when this fails: a link made at the name before the create is followed, and the create
// makes the file it leads to.
func TestALinkMadeAtAMissingNameBeforeTheCreateIsRefused(t *testing.T) {
	t.Parallel()
	root, dir, hook := raced(t, map[string]func(string){stepOpen: func(dir string) {
		plantLink(t, "gone.txt", filepath.Join(dir, "new.txt"))
	}})
	f, err := walkOpen(root, "new.txt", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644, hook)
	refused(t, "Open(new.txt) with a link made at it", f, err)
	if _, err := os.Lstat(filepath.Join(dir, "gone.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the link's target was made: %v", err)
	}
}

// A file made at a missing name and taken away again before the second walk
// is refused, and nothing is made.
//
// What it costs when this fails: a file taken away mid-open is written to as though it were there, and the
// write lands in a file nobody can find.
func TestAFileMadeAndTakenAwayWhileItIsOpenedIsRefused(t *testing.T) {
	t.Parallel()
	root, dir, hook := raced(t, map[string]func(string){
		stepOpen:      func(dir string) { must(t, os.WriteFile(filepath.Join(dir, "new.txt"), nil, 0o644)) },
		stepWalkAgain: func(dir string) { must(t, os.Remove(filepath.Join(dir, "new.txt"))) },
	})
	f, err := walkOpen(root, "new.txt", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644, hook)
	refused(t, "Open(new.txt) made and taken away", f, err)
	if _, err := os.Lstat(filepath.Join(dir, "new.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("new.txt was left: %v", err)
	}
}

// The control: a file another process makes at a missing name between the
// walk and the create is walked again and opened as it is, and the append
// lands after what it holds.
//
// What it costs when this fails: a harmless file made in the same moment makes the open fail, so a program
// that appends to a log loses its line.
func TestAFileMadeAtAMissingNameBeforeTheCreateIsOpened(t *testing.T) {
	t.Parallel()
	root, dir, hook := raced(t, map[string]func(string){stepOpen: func(dir string) {
		must(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("first\n"), 0o644))
	}})
	f, err := walkOpen(root, "new.txt", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644, hook)
	if err != nil {
		t.Fatalf("Open(new.txt) made by another = %v", err)
	}
	if _, err := f.WriteString("second\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	holds(t, dir, "new.txt", "first\nsecond\n")
}

// A missing file opened without os.O_CREATE is fs.ErrNotExist at once, and
// open does not look again: a file another process makes there in between
// is never opened.
//
// What it costs when this fails: an open that was told nothing was there opens what a process put there a
// moment later.
func TestAMissingFileOpenedWithoutCreateIsNotLookedForAgain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	steps := 0
	f, err := walkOpen(root, "new.txt", os.O_RDONLY, 0, func(string) {
		steps++
		must(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644))
	})
	if f != nil {
		_ = f.Close()
	}
	if !errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrOutside) || steps != 0 {
		t.Errorf("open(new.txt) of a missing file = %v after %d steps, want fs.ErrNotExist at once", err, steps)
	}
}

// A folder on the path swapped for a link between the walk and the create
// has the file made in the folder the walk holds, now d-was, never where the
// link led; the second walk finds the link, and the made file is refused
// before a byte is written to it.
//
// What it costs when this fails: a process that swaps state for a link to
// .git at the right moment has a new file, and what is written to it, land
// in .git.
func TestAFileMadeThroughAFolderSwappedForALinkIsRefused(t *testing.T) {
	t.Parallel()
	root, dir, hook := raced(t, map[string]func(string){stepOpen: func(dir string) {
		must(t, os.Rename(filepath.Join(dir, "d"), filepath.Join(dir, "d-was")))
		plantLink(t, "e", filepath.Join(dir, "d"))
	}})
	for _, folder := range []string{"d", "e"} {
		must(t, os.Mkdir(filepath.Join(dir, folder), 0o755))
	}
	f, err := walkOpen(root, filepath.Join("d", "new.txt"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644, hook)
	refused(t, "open(d/new.txt) through a folder swapped for a link", f, err)
	if _, err := os.Lstat(filepath.Join(dir, "e", "new.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a file was made where the link led: %v", err)
	}
}

// A folder swapped for a link before the create, and swapped back before
// the made file is walked again, has the file made in the folder the walk
// holds, and nothing where the link led: the second walk finds the made file
// at its name, and it is opened. On Windows the swap cannot be undone while
// the walk holds the made file (undoSwap), so the second walk meets the link
// and the open is refused, with nothing where the link led either.
//
// What it costs when this fails: a process that swaps state for a link to
// .git and back at the right moments has a new file made in .git, with no
// link left to see.
func TestAFolderSwappedForALinkBeforeTheCreateHasTheFileMadeInTheFolderWalked(t *testing.T) {
	t.Parallel()
	undone := false
	root, dir, hook := raced(t, map[string]func(string){
		stepOpen: func(dir string) {
			must(t, os.Rename(filepath.Join(dir, "d"), filepath.Join(dir, "d-was")))
			plantFolderLink(t, dir, "e", "d")
		},
		stepMade: func(dir string) { undone = undoSwap(t, dir) },
	})
	for _, folder := range []string{"d", "e"} {
		must(t, os.Mkdir(filepath.Join(dir, folder), 0o755))
	}
	f, err := walkOpen(root, filepath.Join("d", "new.txt"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644, hook)
	if !undone {
		refused(t, "open(d/new.txt) through a swap that could not be undone", f, err)
	} else {
		if err != nil {
			t.Fatalf("open(d/new.txt) through a swap undone = %v", err)
		}
		if _, err := f.WriteString("x\n"); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		holds(t, dir, filepath.Join("d", "new.txt"), "x\n")
	}
	if _, err := os.Lstat(filepath.Join(dir, "e", "new.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a file was made where the link led: %v", err)
	}
}

// A folder swapped for a link before the create, and swapped back with
// another file put at the name before the made file is walked again, finds
// no link on the second walk; the made file is refused because the walk
// finds another file there. On Windows the swap cannot be undone (undoSwap),
// and the made file is refused because the walk meets the link.
//
// What it costs when this fails: a program writes to a file that no name in
// the root leads to any more, and the bytes are lost where nobody looks.
func TestAFileMadeThroughASwapUndoneBeforeItIsWalkedIsRefused(t *testing.T) {
	t.Parallel()
	undone := false
	root, dir, hook := raced(t, map[string]func(string){
		stepOpen: func(dir string) {
			must(t, os.Rename(filepath.Join(dir, "d"), filepath.Join(dir, "d-was")))
			plantFolderLink(t, dir, "e", "d")
		},
		stepMade: func(dir string) {
			if undone = undoSwap(t, dir); undone {
				must(t, os.Remove(filepath.Join(dir, "d", "new.txt")))
				must(t, os.WriteFile(filepath.Join(dir, "d", "new.txt"), []byte("other\n"), 0o644))
			}
		},
	})
	for _, folder := range []string{"d", "e"} {
		must(t, os.Mkdir(filepath.Join(dir, folder), 0o755))
	}
	f, err := walkOpen(root, filepath.Join("d", "new.txt"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644, hook)
	refused(t, "open(d/new.txt) through a swap undone before the second walk", f, err)
	if undone {
		holds(t, dir, filepath.Join("d", "new.txt"), "other\n")
	} else if _, err := os.Lstat(filepath.Join(dir, "e", "new.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a file was made where the link led: %v", err)
	}
}

// A folder on the path swapped for a link that stays inside the root, between the walk's look at
// it and its look at the next component, is refused, opened to be written or made: the walk opens
// the folder it looked at by handle and finds the link's target instead. The file behind the link,
// .git/HEAD, keeps every byte, and nothing is made beside it.
//
// What it costs when this fails: a process that swaps state for a link to .git at the right moment
// has a write to state/HEAD land in .git/HEAD, which the walk takes for the file it found.
func TestAFolderSwappedForAnInsideLinkBetweenTwoStepsOfTheWalkIsRefused(t *testing.T) {
	t.Parallel()
	tried := 0
	for name, flag := range map[string]int{
		"HEAD": os.O_WRONLY | os.O_APPEND,
		"new":  os.O_WRONLY | os.O_APPEND | os.O_CREATE,
	} {
		root, dir, hook := raced(t, map[string]func(string){stepWalk: func(dir string) {
			must(t, os.Rename(filepath.Join(dir, "state"), filepath.Join(dir, "state-was")))
			must(t, os.Rename(filepath.Join(dir, "l"), filepath.Join(dir, "state")))
		}})
		for p, body := range map[string]string{"state/HEAD": "state\n", ".git/HEAD": "git\n"} {
			must(t, os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o755))
			must(t, os.WriteFile(filepath.Join(dir, p), []byte(body), 0o644))
		}
		plantLink(t, ".git", filepath.Join(dir, "l"))
		f, err := walkOpen(root, filepath.Join("state", name), flag, 0o644, hook)
		if f != nil {
			_, _ = f.WriteString("x")
		}
		refused(t, "open(state/"+name+") with state swapped for a link to .git mid-walk", f, err)
		holds(t, dir, filepath.Join(".git", "HEAD"), "git\n")
		if _, err := os.Lstat(filepath.Join(dir, ".git", "new")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("a file was made in .git: %v", err)
		}
		tried++
	}
	if tried != 2 {
		t.Fatalf("tried %d of 2 opens", tried)
	}
}

// A file swapped for a link that leads outside the root, between the walk and the open, is
// refused as ErrOutside, not with os.Root's own error, and the file outside keeps every byte.
//
// What it costs when this fails: the refusal reads as an error of the disk, not as a path that
// left the folder, and a caller that tells the two apart with errors.Is takes the wrong turn.
func TestAFileSwappedForALinkOutsideBetweenTheWalkAndTheOpenIsRefused(t *testing.T) {
	t.Parallel()
	out := filepath.Join(t.TempDir(), "out.txt")
	must(t, os.WriteFile(out, []byte("out\n"), 0o644))
	root, _, hook := raced(t, map[string]func(string){stepOpen: func(dir string) {
		must(t, os.Remove(filepath.Join(dir, "a.txt")))
		plantLink(t, out, filepath.Join(dir, "a.txt"))
	}})
	f, err := walkOpen(root, "a.txt", os.O_WRONLY|os.O_TRUNC, 0, hook)
	refused(t, "Open(a.txt) with a link outside swapped in", f, err)
	if got, err := os.ReadFile(out); err != nil || string(got) != "out\n" {
		t.Errorf("the file outside holds %q, %v", got, err)
	}
}

// The swap of TestOpensRacedByAFolderSwappedWithALinkGiveTheInsideFileOrARefusal, forced at each
// step an open reaches, so its answer does not hang on the machine's speed: d, a folder holding
// f.txt, swapped for l, a link to a folder outside the root holding its own f.txt. Between two
// steps of the walk the walk refuses it, as it opens d by what it looked at; just before the open
// the walk opens the inside file from the d it holds, and on Linux and macOS, where Open is the
// kernel's, the kernel refuses it. No answer is the outside file.
//
// What it costs when this fails: a process that swaps a folder for a link out of the root at the
// right moment has a program read a file outside its folder, a moment the race finds by chance.
func TestAFolderSwappedForALinkOutsideAtEachStepOfAnOpenIsRefusedOrGivesTheInsideFile(t *testing.T) {
	t.Parallel()
	tried := 0
	for _, c := range []struct {
		name    string
		open    func(*os.Root, string, int, fs.FileMode, func(string)) (*os.File, error)
		step    string
		refused bool
	}{
		{"Open, just before the open", open, stepOpen, kernelOpens},
		{"the walk, between two steps", walkOpen, stepWalk, true},
		{"the walk, just before the open", walkOpen, stepOpen, false},
	} {
		root, dir, hook := raced(t, map[string]func(string){c.step: func(dir string) {
			must(t, os.Rename(filepath.Join(dir, "d"), filepath.Join(dir, "d-was")))
			must(t, os.Rename(filepath.Join(dir, "l"), filepath.Join(dir, "d")))
		}})
		out := t.TempDir()
		for p, body := range map[string]string{filepath.Join(dir, "d", "f.txt"): "inside", filepath.Join(out, "f.txt"): "outside"} {
			must(t, os.MkdirAll(filepath.Dir(p), 0o755))
			must(t, os.WriteFile(p, []byte(body), 0o644))
		}
		rel, err := filepath.Rel(dir, out)
		must(t, err)
		plantFolderLink(t, dir, rel, "l")
		f, err := c.open(root, filepath.Join("d", "f.txt"), os.O_RDONLY, 0, hook)
		var got []byte
		if f != nil {
			got, err = io.ReadAll(f)
			_ = f.Close()
		}
		switch {
		case c.refused && (f != nil || !errors.Is(err, ErrOutside)):
			t.Errorf("%s, d swapped for a link outside: read %q, %v, want ErrOutside", c.name, got, err)
		case !c.refused && (err != nil || string(got) != "inside"):
			t.Errorf("%s, d swapped for a link outside: read %q, %v, want the inside file", c.name, got, err)
		}
		tried++
	}
	if tried != 3 {
		t.Fatalf("tried %d of 3 opens", tried)
	}
}

// What it costs when this fails: where the system has no open that refuses a link itself, as on
// Windows, ReadCapped walks the name, and a name swapped for a folder after the walk is refused as
// changed but not as ErrNotRegular, so a caller that branches on ErrNotRegular, as the folder row of
// TestReadCappedReadsNothingOutsideWhenTheNameIsSwappedBeforeTheOpen does, takes it for another
// refusal on one system and not on another; and where the system gives the removed file's number to
// the folder, as Linux does, the walk takes the folder for the file walked and opens it.
func TestReadCappedByTheWalkRefusesANameSwappedForAFolderAsNotRegular(t *testing.T) {
	t.Parallel()
	root, _, hook := raced(t, map[string]func(string){stepOpen: func(dir string) {
		must(t, os.Remove(filepath.Join(dir, "a.txt")))
		must(t, os.Mkdir(filepath.Join(dir, "a.txt"), 0o755))
	}})
	f, err := walkOpen(root, "a.txt", os.O_RDONLY|noWait, 0, hook)
	if f != nil {
		_ = f.Close()
	}
	if f != nil || !errors.Is(err, ErrNotRegular) || !errors.Is(err, ErrOutside) {
		t.Errorf("a.txt swapped for a folder between the walk and the open: %v, %v, and want nothing, ErrNotRegular and ErrOutside", f != nil, err)
	}
}

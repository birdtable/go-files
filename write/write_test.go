package write

import (
	"bufio"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// put0 makes path hold body, 0600, for a test to replace.
func put0(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// holds fails the test unless path holds body.
func holds(t *testing.T, path, body string) {
	t.Helper()
	if b, err := os.ReadFile(path); err != nil || string(b) != body {
		t.Errorf("%s holds %q (%v), and want %q", path, b, err, body)
	}
}

// alone fails the test unless dir holds exactly names: no temp file is left beside them.
func alone(t *testing.T, dir string, names ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	got := []string{}
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if err != nil || strings.Join(got, " ") != strings.Join(names, " ") {
		t.Errorf("%s holds %v (%v), and want %v", dir, got, err, names)
	}
}

// What it costs when this fails: a config read while it is written is half a config, and a
// service that loads it refuses to start.
func TestAFileIsReplacedWholeAndNothingIsLeftBeside(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	if err := File(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	holds(t, path, "new")
	permIs(t, path, 0o600)
	alone(t, dir, "app.toml")
}

// A reader that opened the file before the write reads the old bytes to the end; one that opens
// it after reads the new. The reader opens through an os.Root, as the library's own reads do.
//
// What it costs when this fails: a service reloading its config reads half of the old and half of
// the new.
func TestAReaderKeepsTheOldBytes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old bytes")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	r, err := root.Open("app.toml")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := File(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if b, err := io.ReadAll(r); err != nil || string(b) != "old bytes" {
		t.Errorf("the reader read %q (%v), and want the old bytes", b, err)
	}
	holds(t, path, "new")
}

// A missing folder is an error, not a folder made.
//
// What it costs when this fails: a mistyped path quietly makes a folder tree and the program
// writes its files where nobody looks.
func TestAMissingFolderIsAnError(t *testing.T) {
	t.Parallel()
	if err := File(filepath.Join(t.TempDir(), "no", "f"), []byte("x")); err == nil {
		t.Error("a file was written into a folder that is not there")
	}
}

// A writer killed after its temp file is written and before the rename leaves the old file whole.
//
// What it costs when this fails: a crash or an OOM kill mid-write leaves a config that no longer
// parses.
func TestAWriterKilledBeforeTheRenameLeavesTheOldFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	cmd := helperCmd(t, "stop", path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "written" {
		t.Fatalf("the writer said %q (%v), and want written", line, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	holds(t, path, "old")
}

// A link at the name is replaced by the new file, and what it led to is not touched.
//
// What it costs when this fails: a link planted at a config's name makes the write go through to
// a file of the attacker's choosing.
func TestALinkAtTheNameIsReplacedAndItsTargetUntouched(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	put0(t, target, "theirs")
	path := filepath.Join(dir, "app.toml")
	plantLink(t, "target", path)
	if err := File(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(path); err != nil || !fi.Mode().IsRegular() {
		t.Errorf("the name is %v (%v), and want a plain file", fi, err)
	}
	holds(t, path, "new")
	holds(t, target, "theirs")
}

// NoReplace refuses a name that is taken, a link included, with fs.ErrExist, and leaves what is
// there and no temp file.
//
// What it costs when this fails: a key made once is made again over the one in use.
func TestNoReplaceRefusesATakenName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	put0(t, path, "old")
	if err := File(path, []byte("new"), NoReplace()); !errors.Is(err, fs.ErrExist) {
		t.Errorf("NoReplace over a file: %v, and want fs.ErrExist", err)
	}
	holds(t, path, "old")
	alone(t, dir, "key")
	if err := File(filepath.Join(dir, "fresh"), []byte("new"), NoReplace()); err != nil {
		t.Fatal(err)
	}
	holds(t, filepath.Join(dir, "fresh"), "new")
	alone(t, dir, "fresh", "key")
}

// What it costs when this fails: a link at a key's name lets NoReplace write a key through it.
func TestNoReplaceRefusesALinkAtTheName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	put0(t, target, "theirs")
	path := filepath.Join(dir, "key")
	plantLink(t, "target", path)
	if err := File(path, []byte("new"), NoReplace()); !errors.Is(err, fs.ErrExist) {
		t.Errorf("NoReplace over a link: %v, and want fs.ErrExist", err)
	}
	holds(t, target, "theirs")
	if fi, err := os.Lstat(path); err != nil || fi.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("the link is %v (%v), and want it left", fi, err)
	}
}

// Of writers racing to make one name with NoReplace, one wins and the rest get fs.ErrExist; the
// file is the winner's.
//
// What it costs when this fails: two first runs each make a key, and one of them holds a key
// that is not the one on disk.
func TestNoReplaceRacersGiveOneWinner(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	const racers = 16
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := range racers {
		wg.Go(func() { errs[i] = File(path, []byte{byte('a' + i)}, NoReplace()) })
	}
	wg.Wait()
	won := -1
	for i, err := range errs {
		switch {
		case err == nil && won < 0:
			won = i
		case err == nil:
			t.Errorf("racers %d and %d both won", won, i)
		case !errors.Is(err, fs.ErrExist):
			t.Errorf("racer %d: %v, and want fs.ErrExist", i, err)
		}
	}
	if won < 0 {
		t.Fatal("no racer won")
	}
	holds(t, path, string(rune('a'+won)))
	alone(t, dir, "key")
}

// Check sees the temp file from its start, and its error leaves the old file and no temp file.
//
// What it costs when this fails: a config that fails its own parse is put in place anyway.
func TestAFailingCheckLeavesTheOldFileAndNoTemp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	bad := errors.New("does not parse")
	var seen string
	err := File(path, []byte("new"), Check(func(f *os.File) error {
		b, err := io.ReadAll(f)
		seen = string(b)
		return errors.Join(err, bad)
	}))
	if !errors.Is(err, bad) {
		t.Errorf("a failing Check: %v, and want its error", err)
	}
	if seen != "new" {
		t.Errorf("Check read %q, and want the new bytes", seen)
	}
	holds(t, path, "old")
	alone(t, dir, "app.toml")
}

// Check runs only on the file the write made: a file renamed over the temp name after its close
// is refused, and Check, on a descriptor held since the write, never sees it.
//
// What it costs when this fails: whoever can write the folder puts a file of their own at the temp
// name, and the caller's Check passes on bytes that are not the ones written.
func TestCheckNeverSeesAFileSwappedInAtTheTempName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	theirs := filepath.Join(dir, "theirs")
	put0(t, theirs, "theirs")
	var seen []string
	err := File(path, []byte("new"), Check(func(f *os.File) error {
		b, err := io.ReadAll(f)
		seen = append(seen, string(b))
		return err
	}), stepWith(func(at string) {
		if at != "closed" {
			return
		}
		tmp, err := filepath.Glob(filepath.Join(dir, ".app.toml.*.tmp"))
		if err != nil || len(tmp) != 1 {
			t.Fatalf("the temp files %v, %v, and want one", tmp, err)
		}
		// By the root: on Windows its rename has POSIX semantics, which a handle the write holds
		// with FILE_SHARE_DELETE does not stop; MoveFileEx, os.Rename's, refuses a file held open.
		r, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		if err := r.Rename("theirs", filepath.Base(tmp[0])); err != nil {
			t.Fatal(err)
		}
	}))
	if err == nil || !strings.Contains(err.Error(), "swapped") {
		t.Errorf("a file renamed over the temp name: %v, and want it refused", err)
	}
	if len(seen) > 1 || len(seen) == 1 && seen[0] != "new" {
		t.Errorf("Check read %q, and want it never run on another file", seen)
	}
	holds(t, path, "old")
	alone(t, dir, "app.toml")
}

// An error from FileFrom's reader leaves the old file and no temp file.
//
// What it costs when this fails: a download cut off halfway replaces a whole file with its half.
func TestAFailingReaderLeavesTheOldFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	cut := errors.New("cut off")
	if err := FileFrom(path, io.MultiReader(strings.NewReader("half"), failing{cut})); !errors.Is(err, cut) {
		t.Errorf("a failing reader: %v, and want its error", err)
	}
	holds(t, path, "old")
	alone(t, dir, "app.toml")
	if err := FileFrom(path, strings.NewReader("whole")); err != nil {
		t.Fatal(err)
	}
	holds(t, path, "whole")
}

// failing is a reader that fails with its error.
type failing struct{ err error }

func (f failing) Read([]byte) (int, error) { return 0, f.err }

// IfHolds writes when the file holds what the digest says, and refuses with ErrChanged when it
// holds anything else or is gone, writing nothing.
//
// What it costs when this fails: a program writes over a change a person made since it read the
// file.
func TestIfHoldsRefusesAStaleDigest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "edited")
	if err := File(path, []byte("new"), IfHolds(sha256.Sum256([]byte("read")))); !errors.Is(err, ErrChanged) {
		t.Errorf("a stale digest: %v, and want ErrChanged", err)
	}
	holds(t, path, "edited")
	alone(t, dir, "app.toml")
	if err := File(filepath.Join(dir, "gone"), []byte("new"), IfHolds(sha256.Sum256(nil))); !errors.Is(err, ErrChanged) {
		t.Errorf("a file that is gone: %v, and want ErrChanged", err)
	}
	alone(t, dir, "app.toml")
	if err := File(path, []byte("new"), IfHolds(sha256.Sum256([]byte("edited")))); err != nil {
		t.Fatal(err)
	}
	holds(t, path, "new")
}

// IfHolds hashes only a plain file: a link at the name is refused, and not read through.
//
// What it costs when this fails: a digest of a file the link leads to passes, and the write goes
// over a link a person put there.
func TestIfHoldsRefusesALinkAtTheName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	put0(t, target, "same")
	path := filepath.Join(dir, "app.toml")
	plantLink(t, "target", path)
	if err := File(path, []byte("new"), IfHolds(sha256.Sum256([]byte("same")))); err == nil {
		t.Error("IfHolds read through a link")
	}
	holds(t, target, "same")
}

// A folder that cannot be flushed after the rename fails the write with ErrNotDurable, the new
// file in place; NoSync flushes nothing, so it cannot fail so.
//
// What it costs when this fails: a program told the write failed writes again, or one told it
// held takes a power cut that brings the old file back.
func TestAFolderNotFlushedSaysTheFileIsInPlace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	broke := errors.New("flush refused")
	fail := syncWith(func(*os.Root, string) error { return broke })
	if err := File(path, []byte("new"), fail); !errors.Is(err, ErrNotDurable) || !errors.Is(err, broke) {
		t.Errorf("a refused flush: %v, and want ErrNotDurable with its cause", err)
	}
	holds(t, path, "new")
	if err := File(path, []byte("newer"), fail, NoSync()); err != nil {
		t.Errorf("NoSync flushed: %v", err)
	}
	holds(t, path, "newer")
	alone(t, dir, "app.toml")
}

// FileIn refuses a name that leads out of its root with the os package's own error, whose text
// the os package keeps.
//
// What it costs when this fails: a name from a request ("../../etc/x") is written outside the
// folder the program gave.
func TestFileInRefusesAPathOutOfItsRoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inner := filepath.Join(dir, "in")
	if err := os.Mkdir(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(inner)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	err = FileIn(root, filepath.Join("..", "out"), []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "path escapes from parent") {
		t.Errorf("a name out of the root: %v, and want the os package's path escapes from parent", err)
	}
	alone(t, dir, "in")
	if !panics(func() { _ = FileIn(nil, "x", []byte("x")) }) {
		t.Error("FileIn with no root did not panic, as a nil argument does")
	}
	if err := FileIn(root, "x", []byte("x")); err != nil {
		t.Fatal(err)
	}
	holds(t, filepath.Join(inner, "x"), "x")
}

// What it costs when this fails: a link inside the root that leads out of it carries the write
// out.
func TestFileInRefusesAFolderLinkOutOfItsRoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inner := filepath.Join(dir, "in")
	if err := os.Mkdir(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	plantLink(t, dir, filepath.Join(inner, "up"))
	root, err := os.OpenRoot(inner)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := FileIn(root, filepath.Join("up", "out"), []byte("x")); err == nil {
		t.Error("FileIn wrote through a link out of its root")
	}
	alone(t, dir, "in")
}

// Keep makes a copy beside at the caller's name, and PutBack restores its bytes and mode over the file written
// since; PutBack refuses with ErrChanged when the file holds anything else.
//
// What it costs when this fails: an undo puts back the wrong bytes, or goes over a change made
// after the write it undoes.
func TestKeepThenPutBackRestoresTheFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "before")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	kept, err := Keep(path, dir, "app.toml.before-v2")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "app.toml.before-v2"); kept != want {
		t.Errorf("the copy is %s, and want %s", kept, want)
	}
	holds(t, kept, "before")
	permIs(t, kept, 0o640)
	if err := File(path, []byte("after")); err != nil {
		t.Fatal(err)
	}
	if err := PutBack(kept, path, sha256.Sum256([]byte("other"))); !errors.Is(err, ErrChanged) {
		t.Errorf("PutBack over a changed file: %v, and want ErrChanged", err)
	}
	holds(t, path, "after")
	if err := PutBack(kept, path, sha256.Sum256([]byte("after"))); err != nil {
		t.Fatal(err)
	}
	holds(t, path, "before")
	permIs(t, path, 0o640)
	alone(t, dir, "app.toml", filepath.Base(kept))
}

// A taken name, a plain file or a link, is refused with fs.ErrExist, never written over or
// followed; a name that is not one plain name in the folder is refused before anything is
// opened. Nothing is written either way.
//
// What it costs when this fails: a second copy writes over the first, a link planted at the
// copy's name carries the file's bytes elsewhere, a name with a separator or a colon puts the
// copy outside the caller's folder or in a stream of another file, or a name ending in a dot or
// a space is made on Windows without it, so the path Keep answers names another file.
func TestKeepRefusesATakenNameALinkAndANameThatIsNotPlain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "one")
	if _, err := Keep(path, dir, "app.toml.v12"); err != nil {
		t.Fatal(err)
	}
	if _, err := Keep(path, dir, "app.toml.v12"); !errors.Is(err, fs.ErrExist) {
		t.Errorf("Keep at a taken name: %v, and want fs.ErrExist", err)
	}
	target := filepath.Join(dir, "target")
	put0(t, target, "theirs")
	plantLink(t, "target", filepath.Join(dir, "app.toml.v13"))
	if _, err := Keep(path, dir, "app.toml.v13"); !errors.Is(err, fs.ErrExist) {
		t.Errorf("Keep at a link: %v, and want fs.ErrExist", err)
	}
	holds(t, target, "theirs")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, name := range []string{"", ".", "..", "sub/app.toml", `sub\app.toml`, "../app.toml", "app.toml:v14", "app\x00toml", "app.toml.v14.", "app.toml.v14 "} {
		if _, err := Keep(path, dir, name); err == nil || errors.Is(err, fs.ErrExist) {
			t.Errorf("Keep at %q: %v, and want a refusal of the name", name, err)
		}
		ran++
	}
	if ran != 10 {
		t.Fatalf("ran %d of 10 names", ran)
	}
	holds(t, filepath.Join(dir, "app.toml.v12"), "one")
	alone(t, dir, "app.toml", "app.toml.v12", "app.toml.v13", "sub", "target")
	alone(t, filepath.Join(dir, "sub"))
}

// Keep copies only a plain file: a link at the file is refused, and a missing file makes no copy.
//
// What it costs when this fails: a copy holds what a planted link led to, and PutBack later
// writes that over the file.
func TestKeepRefusesALinkAndAMissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	put0(t, target, "theirs")
	path := filepath.Join(dir, "app.toml")
	plantLink(t, "target", path)
	if _, err := Keep(path, dir, "app.toml.kept"); err == nil {
		t.Error("Keep copied through a link")
	}
	if _, err := Keep(filepath.Join(dir, "gone"), dir, "gone.kept"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Keep of a missing file: %v, and want fs.ErrNotExist", err)
	}
	alone(t, dir, "app.toml", "target")
}

// A folder swapped for a link out of the root after the temp file is written, before the rename,
// with the temp file moved out beside the link's target, is refused at the rename: the name is
// reached beneath the root alone, so nothing outside is written over. On Windows the rename is
// written through to the disk, and still beneath the root.
//
// What it costs when this fails: a program that writes by a name beneath a root another account
// can change puts its file over one outside the root, by a path, as MoveFileEx would.
func TestFileInRefusesAFolderSwappedForALinkBeforeTheRename(t *testing.T) {
	t.Parallel()
	dir, out := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	put0(t, filepath.Join(dir, "sub", "app.toml"), "old")
	put0(t, filepath.Join(out, "app.toml"), "outside")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	swapped := false
	err = FileIn(root, filepath.Join("sub", "app.toml"), []byte("new"), stepWith(func(at string) {
		if at != "written" {
			return
		}
		// The temp file goes out first: on Windows the grip holds it, and its folder with it, in place.
		tmps, err := filepath.Glob(filepath.Join(dir, "sub", ".app.toml.*.tmp"))
		if err != nil || len(tmps) != 1 {
			t.Fatalf("the temp file: %v (%v)", tmps, err)
		}
		if err := os.Rename(tmps[0], filepath.Join(out, filepath.Base(tmps[0]))); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(dir, "sub"), filepath.Join(dir, "moved")); err != nil {
			t.Fatal(err)
		}
		plantLink(t, out, filepath.Join(dir, "sub"))
		swapped = true
	}))
	if !swapped {
		t.Fatal("the folder was never swapped")
	}
	if err == nil {
		t.Error("FileIn put its file in place through a folder swapped for a link out of its root")
	}
	holds(t, filepath.Join(out, "app.toml"), "outside")
	holds(t, filepath.Join(dir, "moved", "app.toml"), "old")
}

// SyncFolder flushes a folder beneath its root, at any depth, and refuses, on every system, a
// name that is missing, one that is not a folder, and a link that leads out of the root.
//
// What it costs when this fails: a program flushes the wrong thing, or nothing, and believes a
// removal or a new file will survive a power cut; or a link has it open a folder outside its root.
func TestSyncFolderFlushesAFolderBeneathTheRootAndRefusesTheRest(t *testing.T) {
	t.Parallel()
	dir, out := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o700); err != nil {
		t.Fatal(err)
	}
	put0(t, filepath.Join(dir, "file"), "x")
	plantLink(t, out, filepath.Join(dir, "out"))
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, ok := range []string{".", "a", filepath.Join("a", "b")} {
		if err := SyncFolder(root, ok); err != nil {
			t.Errorf("SyncFolder %q: %v, and want nil", ok, err)
		}
	}
	if err := SyncFolder(root, "missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("SyncFolder of a missing name: %v, and want fs.ErrNotExist", err)
	}
	if err := SyncFolder(root, "file"); err == nil || !strings.Contains(err.Error(), "is not a folder") {
		t.Errorf("SyncFolder of a file: %v, and want it refused as not a folder", err)
	}
	if err := SyncFolder(root, "out"); err == nil {
		t.Error("SyncFolder opened a folder outside its root through a link")
	}
}

// A temp name that cannot be removed once the file is linked at its name under NoReplace is an error
// that says the file is in place and its temp file is left, and the rest of the write runs after
// it: the placed step is reached and the folder is flushed once, as after a close that failed.
// Returning place's error from put before stays and the flush, as it did, turns this red.
//
// What it costs when this fails: the file is at its name and the folder is never flushed, so a
// power cut takes a file the caller was told is in place; on Windows the placed name is left
// writable where its mode has no owner write.
func TestATempNameThatCannotBeRemovedUnderNoReplaceStillFlushesTheFolder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	denied := func(*os.Root, string) error { return fs.ErrPermission }
	flushed, placed := 0, false
	count := syncWith(func(*os.Root, string) error { flushed++; return nil })
	err := File(path, []byte("new"), NoReplace(), removeWith(denied), count, stepWith(func(at string) { placed = placed || at == "placed" }))
	if !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "app.toml is in place, and its temp file is left") {
		t.Errorf("a temp name that cannot be removed: %v, and want an error that wraps the remove's and says the file is in place and its temp file left", err)
	}
	if !placed {
		t.Error("a failed remove of the temp name after the link never reached the placed step")
	}
	if flushed != 1 {
		t.Errorf("the folder was flushed %d times after a failed remove of the temp name, and want once", flushed)
	}
	holds(t, path, "new")
	temps, gerr := filepath.Glob(filepath.Join(dir, ".app.toml.*.tmp"))
	if gerr != nil || len(temps) != 1 {
		t.Fatalf("the temp files are %v (%v), and want the one left", temps, gerr)
	}
	holds(t, temps[0], "new")
	alone(t, dir, filepath.Base(temps[0]), "app.toml")
}

// A flush that fails after a temp name could not be removed gives both: the write is ErrNotDurable,
// and still says its temp file is left.
//
// What it costs when this fails: one of the two is dropped, and a caller either trusts a file that
// may not survive a power cut or never clears the temp name.
func TestAFailedFlushAfterATempNameIsLeftSaysBoth(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	denied := func(*os.Root, string) error { return fs.ErrPermission }
	err := File(filepath.Join(dir, "app.toml"), []byte("new"), NoReplace(), removeWith(denied),
		syncWith(func(*os.Root, string) error { return errors.New("no flush") }))
	if !errors.Is(err, ErrNotDurable) || !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "its temp file is left") {
		t.Errorf("a failed flush after a temp name is left: %v, and want ErrNotDurable joined with the temp file left", err)
	}
}

// A grip whose close fails once the file is in place is an error that says the file is in place, for
// a write that replaces and for NoReplace, and the file is there with its bytes; the rest of the
// write runs after it, so the folder is flushed once.
//
// What it costs when this fails: the write reports an error with no word that its file is at the
// name, and a caller that retries or removes it works from a wrong picture; or the close's error is
// dropped and the caller never hears of it.
func TestAGripThatFailsToCloseAfterTheFileIsInPlaceSaysSo(t *testing.T) {
	t.Parallel()
	for way, opts := range map[string][]Option{"replace": nil, "NoReplace": {NoReplace()}} {
		t.Run(way, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "app.toml")
			boom := errors.New("the close failed")
			flushed := 0
			count := syncWith(func(*os.Root, string) error { flushed++; return nil })
			err := File(path, []byte("new"), append(opts, count, closeWith(func(error) error { return boom }))...)
			if !errors.Is(err, boom) || !strings.Contains(err.Error(), "is in place") {
				t.Errorf("a grip that fails to close after the file is in place: %v, and want an error that wraps the close's and says the file is in place", err)
			}
			if flushed != 1 {
				t.Errorf("the folder was flushed %d times after a failed close, and want once", flushed)
			}
			holds(t, path, "new")
			alone(t, dir, "app.toml")
		})
	}
}

package write

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// A read-only file is replaced, and the new file is not read-only.
//
// What it costs when this fails: a config a person or an installer marked read-only can never be
// written again.
func TestAReadOnlyFileIsReplaced(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := File(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	holds(t, path, "new")
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm()&0o200 == 0 {
		t.Errorf("the new file is %v (%v), and want it writable", fi, err)
	}
	alone(t, dir, "app.toml")
}

// Owner with any ids but exactly -1 and -1, Owner(1000, 1000), Owner(0, 0) and Owner(-2, -2) among
// them, is refused with
// ErrNoOwner, which matches errors.ErrUnsupported, in File and FileIn, and nothing is written;
// Owner(-1, -1), which asks for no change, is taken, and the file is this account's with the
// owner-only list.
//
// What it costs when this fails: a program that gives a file to a service account believes it
// did, and the file stays its writer's; or a program that passes Owner(-1, -1) on every system
// cannot write on Windows.
func TestOwnerIsTakenOnlyAsNoChange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	for _, ids := range [][2]int{{1000, 1000}, {0, 0}, {0, -1}, {-1, 0}, {-2, -2}, {-1, -2}, {-2, -1}} {
		for what, err := range map[string]error{
			"File":   File(path, []byte("new"), Owner(ids[0], ids[1]), NoSync()),
			"FileIn": FileIn(root, "app.toml", []byte("new"), Owner(ids[0], ids[1]), NoSync()),
		} {
			if !errors.Is(err, ErrNoOwner) || !errors.Is(err, errors.ErrUnsupported) {
				t.Errorf("%s with Owner(%d, %d) on Windows: %v, and want ErrNoOwner matching errors.ErrUnsupported", what, ids[0], ids[1], err)
			}
		}
	}
	holds(t, path, "old")
	alone(t, dir, "app.toml")
	if err := File(path, []byte("new"), Owner(-1, -1), NoSync()); err != nil {
		t.Fatalf("File with Owner(-1, -1): %v", err)
	}
	holds(t, path, "new")
	ownerOnly(t, path)
	if err := FileIn(root, "app.toml", []byte("newer"), Owner(-1, -1), NoSync()); err != nil {
		t.Fatalf("FileIn with Owner(-1, -1): %v", err)
	}
	holds(t, path, "newer")
	ownerOnly(t, path)
	alone(t, dir, "app.toml")
}

// A temp file another process holds for a moment, as a scanner does, is a sharing violation at the
// grip's open in ready, which is tried again, and the file is put in place once it is let go.
//
// What it costs when this fails: on a machine with a scanner, a write fails now and then, and the
// program says it could not save a file it could have.
func TestATempFileHeldForAMomentIsPutInPlace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	held := false
	err := File(path, []byte("new"), stepWith(func(at string) {
		if at != "closed" {
			return
		}
		tmps, err := filepath.Glob(filepath.Join(dir, ".app.toml.*.tmp"))
		if err != nil || len(tmps) != 1 {
			t.Fatalf("the temp file: %v (%v)", tmps, err)
		}
		name, err := windows.UTF16PtrFromString(tmps[0])
		if err != nil {
			t.Fatal(err)
		}
		// Shared for reading alone, not for delete, as a scanner holds a file it reads.
		h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil,
			windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if err != nil {
			t.Fatal(err)
		}
		held = true
		time.AfterFunc(20*time.Millisecond, func() { _ = windows.CloseHandle(h) })
	}))
	if !held {
		t.Fatal("the temp file was never held")
	}
	if err != nil {
		t.Fatalf("a temp file held for a moment: %v, and want it put in place once let go", err)
	}
	holds(t, path, "new")
	alone(t, dir, "app.toml")
}

// A log held for a moment by another process that shares it for reading and writing but not for a
// move, as a scanner or a tail does, is moved to .1 once let go, and the line goes into a new log.
//
// What it costs when this fails: on a machine with a scanner, a line at the cap fails now and
// then, and an audit line is not written.
func TestALogHeldForAMomentIsMovedAside(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := filepath.Join(dir, "audit.log")
	put0(t, log, "old\n")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	held := false
	err = appendWith(t.Context(), root, "audit.log", []byte("line"), 6, settle([]Option{asideWith("audit.log.1"), stepWith(func(at string) {
		if at != "held" || held {
			return
		}
		name, err := windows.UTF16PtrFromString(log)
		if err != nil {
			t.Fatal(err)
		}
		h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
			windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if err != nil {
			t.Fatal(err)
		}
		held = true
		time.AfterFunc(20*time.Millisecond, func() { _ = windows.CloseHandle(h) })
	})}))
	if !held {
		t.Fatal("the log was never held")
	}
	if err != nil {
		t.Fatalf("a log held for a moment at the cap: %v, and want it moved once let go", err)
	}
	holds(t, log, "line\n")
	holds(t, log+".1", "old\n")
}

// An old file a reader holds open for a moment through os.Open, which does not share it for
// delete, is replaced once the reader lets go: the rename over it is an access denial, which is
// tried again, and the reader's handle read the old bytes.
//
// What it costs when this fails: a program that rewrites a file another program reads (a config,
// a note) fails to save it whenever the reader has it open.
func TestAnOldFileAReaderHoldsForAMomentIsReplaced(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	read := make(chan string, 1)
	held := false
	err := File(path, []byte("new"), stepWith(func(at string) {
		if at != "written" {
			return
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		held = true
		time.AfterFunc(20*time.Millisecond, func() {
			b, err := io.ReadAll(f)
			_ = f.Close()
			if err != nil {
				b = []byte(err.Error())
			}
			read <- string(b)
		})
	}))
	if !held {
		t.Fatal("the old file was never held")
	}
	if err != nil {
		t.Fatalf("an old file a reader held for a moment: %v, and want it replaced once let go", err)
	}
	if got := <-read; got != "old" {
		t.Errorf("the reader's handle read %q, and want the old bytes", got)
	}
	holds(t, path, "new")
	alone(t, dir, "app.toml")
}

// An old file a reader holds open past the tries is not replaced: the access denial reaches the
// caller as it is, the old file stays, and no temp file is left.
//
// What it costs when this fails: a write that could not be put in place says it was, or leaves a
// temp file beside the name for every refused write.
func TestAnOldFileAReaderHoldsPastTheTriesStaysAndTheDenialIsSaid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	err = File(path, []byte("new"))
	_ = f.Close()
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Errorf("an old file a reader holds past the tries: %v, and want ERROR_ACCESS_DENIED", err)
	}
	holds(t, path, "old")
	alone(t, dir, "app.toml")
}

// The pin opens the held log by its name and takes nothing else there: a link to the held log, a
// folder or another file put at the name after the match gives no pin and no error, so Append goes
// back to the look. The link is not followed (FILE_OPEN_REPARSE_POINT), and the folder and the
// other file are not the held log (os.SameFile).
//
// What it costs when this fails: a swap after the match has the pin take what the swapper put at
// the name, the count sees the held log's one name through the link or the other file's, and the
// line lands in a file of the swapper's choosing.
func TestThePinTakesNothingButTheHeldLogAtItsName(t *testing.T) {
	t.Parallel()
	for row, put := range map[string]func(log string) error{
		"a link to the held log": func(log string) error { return os.Symlink("target", log) },
		"a folder":               func(log string) error { return os.Mkdir(log, 0o700) },
		"another file":           func(log string) error { return os.WriteFile(log, []byte("other"), 0o600) },
	} {
		t.Run(row, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			target := filepath.Join(dir, "target")
			put0(t, target, "theirs")
			mine, err := os.Stat(target)
			if err != nil {
				t.Fatal(err)
			}
			if err := put(filepath.Join(dir, "log")); err != nil {
				t.Fatal(err)
			}
			unpin, err := pin(root, "log", mine)
			if unpin != nil {
				t.Errorf("%s at the name is pinned (%v), and want no pin", row, errors.Join(err, unpin()))
			} else if err != nil {
				t.Errorf("%s at the name: %v, and want no pin and no error", row, err)
			}
		})
	}
}

// An open of the held log by its name that is refused for any reason but the name gone, here a
// sharing violation from another handle that holds the log for delete, reaches the caller as
// itself, and the log gets no line.
//
// What it costs when this fails: Append goes back to the look a thousand times, says the log was
// moved by another writer when none moved it, and the refusal that stopped it is never said.
func TestAPinRefusedReachesTheCallerAsItself(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := filepath.Join(dir, "audit.log")
	put0(t, log, "old\n")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	name, err := windows.UTF16PtrFromString(log)
	if err != nil {
		t.Fatal(err)
	}
	// Shared for all, so Append's open and hold go through, and holding delete, which the pin does not share.
	h, err := windows.CreateFile(name, windows.DELETE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	err = Append(t.Context(), root, "audit.log", []byte("line"), 100, Aside("audit.log.1"))
	_ = windows.CloseHandle(h)
	if !errors.Is(err, windows.STATUS_SHARING_VIOLATION) {
		t.Errorf("a pin refused by a handle that holds delete: %v, and want STATUS_SHARING_VIOLATION as it is", err)
	}
	holds(t, log, "old\n")
}

// LogMode is refused with ErrNoMode, matching errors.ErrUnsupported, before the log is opened or
// made, so no line is written; Append without it writes.
//
// What it costs when this fails: a service believes its audit log is 0640 while it took its
// folder's access list.
func TestLogModeIsRefusedWithErrNoModeAndNoLineIsWritten(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	err = Append(t.Context(), root, "audit.log", []byte("line"), 100, Aside("audit.log.1"), LogMode(0o640))
	if !errors.Is(err, ErrNoMode) || !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Append with LogMode on Windows: %v, and want ErrNoMode matching errors.ErrUnsupported", err)
	}
	alone(t, dir)
	put0(t, filepath.Join(dir, "audit.log"), "old\n")
	err = Append(t.Context(), root, "audit.log", []byte("line"), 100, Aside("audit.log.1"), LogMode(0o600))
	if !errors.Is(err, ErrNoMode) {
		t.Errorf("Append with LogMode to a log already there: %v, and want ErrNoMode", err)
	}
	holds(t, filepath.Join(dir, "audit.log"), "old\n")
	if err := Append(t.Context(), root, "audit.log", []byte("line"), 100, Aside("audit.log.1")); err != nil {
		t.Fatalf("Append without LogMode: %v", err)
	}
	holds(t, filepath.Join(dir, "audit.log"), "old\nline\n")
}

// Mode 0o644, 0o600 and 0o444 are taken in File and FileIn, with a file at the name and without:
// the file has the owner-only list, narrower than each asks for group and other, and 0o444, with
// no owner write, makes it read-only, while 0o644 and 0o600 do not. Keep and PutBack take Mode, as
// their mode is the file's they copy.
//
// What it costs when this fails: a program that writes a config 0o644 on every system cannot write
// it on Windows; or one that writes a file 0o444 so it is not changed by mistake finds it
// writable; or a file asked for 0o644 lets other accounts in.
func TestModeGivesTheOwnerOnlyListAndOwnerWriteIsTheReadOnlyAttribute(t *testing.T) {
	t.Parallel()
	for _, perm := range []fs.FileMode{0o644, 0o600, 0o444} {
		dir := t.TempDir()
		path := filepath.Join(dir, "app.toml")
		check := func(what, body string) {
			t.Helper()
			holds(t, path, body)
			ownerOnly(t, path)
			if got, want := isReadOnly(t, path), perm&0o200 == 0; got != want {
				t.Errorf("%s with Mode(%#o): read-only %v, and want %v", what, perm, got, want)
			}
		}
		if err := File(path, []byte("new"), Mode(perm), NoSync()); err != nil {
			t.Fatalf("File with Mode(%#o) and no file there: %v", perm, err)
		}
		check("File with no file there", "new")
		if err := File(path, []byte("newer"), Mode(perm), NoSync()); err != nil {
			t.Fatalf("File with Mode(%#o): %v", perm, err)
		}
		check("File", "newer")
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		err = FileIn(root, "app.toml", []byte("newest"), Mode(perm), NoSync())
		if cerr := root.Close(); cerr != nil {
			t.Fatal(cerr)
		}
		if err != nil {
			t.Fatalf("FileIn with Mode(%#o): %v", perm, err)
		}
		check("FileIn", "newest")
		alone(t, dir, "app.toml")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	kept, err := Keep(path, dir, "app.toml.kept", Mode(0o600), NoSync())
	if err != nil {
		t.Fatalf("Keep with Mode(0o600): %v", err)
	}
	if err := File(path, []byte("after"), NoSync()); err != nil {
		t.Fatal(err)
	}
	if err := PutBack(kept, path, sha256.Sum256([]byte("after")), Mode(0o600), NoSync()); err != nil {
		t.Fatalf("PutBack with Mode(0o600): %v", err)
	}
	holds(t, path, "old")
	ownerOnly(t, path)
	alone(t, dir, "app.toml", filepath.Base(kept))
}

// A Mode the owner-only list would give more than is refused with ErrNoMode, which matches
// errors.ErrUnsupported, before anything is made: 0o200 and 0o000, which give the owner no read,
// and 0o4755 as written, and 0o755 with fs.ModeSetuid, fs.ModeSetgid or fs.ModeSticky, in File, FileIn, Keep and PutBack, with
// a file at the name and without.
//
// What it costs when this fails: a program that writes a file 0o200 so it cannot read it back, or
// asks for setuid, believes it has that, while the file is the owner's to read and run as itself.
func TestAModeTheOwnerOnlyListWouldGiveMoreThanIsRefusedWithErrNoMode(t *testing.T) {
	t.Parallel()
	for _, perm := range []fs.FileMode{0o200, 0o000, 0o4755, fs.ModeSetuid | 0o755, fs.ModeSetgid | 0o755, fs.ModeSticky | 0o755} {
		dir := t.TempDir()
		path := filepath.Join(dir, "app.toml")
		refused := func(what string, err error) {
			t.Helper()
			if !errors.Is(err, ErrNoMode) || !errors.Is(err, errors.ErrUnsupported) {
				t.Errorf("%s with Mode(%#o) on Windows: %v, and want ErrNoMode matching errors.ErrUnsupported", what, uint32(perm), err)
			}
		}
		refused("File with no file there", File(path, []byte("new"), Mode(perm), NoSync()))
		alone(t, dir)
		put0(t, path, "old")
		refused("File", File(path, []byte("new"), Mode(perm), NoSync()))
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		refused("FileIn", FileIn(root, "app.toml", []byte("new"), Mode(perm), NoSync()))
		if err := root.Close(); err != nil {
			t.Fatal(err)
		}
		_, err = Keep(path, dir, "app.toml.kept", Mode(perm), NoSync())
		refused("Keep", err)
		holds(t, path, "old")
		alone(t, dir, "app.toml")
		kept, err := Keep(path, dir, "app.toml.kept", NoSync())
		if err != nil {
			t.Fatal(err)
		}
		if err := File(path, []byte("after"), NoSync()); err != nil {
			t.Fatal(err)
		}
		refused("PutBack", PutBack(kept, path, sha256.Sum256([]byte("after")), Mode(perm), NoSync()))
		holds(t, path, "after")
		alone(t, dir, "app.toml", filepath.Base(kept))
	}
}

// A write that is not NoSync renames its file written through to the disk, by File and by FileIn,
// and reports nothing; a NoSync write renames it plainly.
//
// What it costs when this fails: a program told a write was flushed takes a power cut that brings
// the old file back, as Windows has no folder flush to stand for it.
func TestADurableWriteIsRenamedWrittenThrough(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	opened := 0
	seen := throughWith(func(open error) error { opened++; return open })
	if err := File(path, []byte("new"), seen); err != nil {
		t.Fatalf("a durable write: %v", err)
	}
	holds(t, path, "new")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	if err := FileIn(root, "app.toml", []byte("newer"), seen); err != nil {
		t.Fatalf("a durable FileIn: %v", err)
	}
	holds(t, path, "newer")
	if opened != 2 {
		t.Errorf("the write-through rename was taken %d times by two durable writes, and want 2", opened)
	}
	if err := File(path, []byte("newest"), seen, NoSync()); err != nil {
		t.Fatalf("a NoSync write: %v", err)
	}
	holds(t, path, "newest")
	if opened != 2 {
		t.Error("a NoSync write took the write-through rename, and want the plain one")
	}
	alone(t, dir, "app.toml")
}

// A volume that refuses the write-through open gets the plain rename: the file is in place, over a
// read-only one too, no temp file is left, and the write reports ErrNotDurable. Any other failure
// of that open is the write's error, with the old file left.
//
// What it costs when this fails: on a volume that cannot write a rename through, every write fails
// and is done again, or is said to be durable when it is not.
func TestARefusedWriteThroughSaysErrNotDurable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	refuse := throughWith(func(error) error { return windows.STATUS_INVALID_PARAMETER })
	err := File(path, []byte("new"), refuse)
	if !errors.Is(err, ErrNotDurable) || !errors.Is(err, errNotThrough) {
		t.Errorf("a refused write-through: %v, and want ErrNotDurable with errNotThrough", err)
	}
	holds(t, path, "new")
	alone(t, dir, "app.toml")
	fail := throughWith(func(error) error { return windows.STATUS_OBJECT_NAME_NOT_FOUND })
	if err := File(path, []byte("newer"), fail); err == nil || errors.Is(err, ErrNotDurable) {
		t.Errorf("a write-through open that fails: %v, and want its error, not ErrNotDurable", err)
	}
	holds(t, path, "new")
	alone(t, dir, "app.toml")
}

// A name the rename would not take as os.Root takes it is refused before anything is opened, with
// NoSync and without: a device name, a trailing dot or space, a trailing or doubled separator, a ..
// The folder is left as it was: the old file holds its bytes and mode, and no file or temp is made.
//
// What it costs when this fails: a durable write makes a file named NUL that nothing can read or
// remove, or replaces app.toml. while IfHolds hashed and KeepMode read app.toml, which it leaves
// writable.
func TestANameTheRenameWouldReadAnotherWayIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	old := sha256.Sum256([]byte("old"))
	escapes := []string{"NUL", "CON", "AUX", "PRN", "COM1", "LPT1", "sub/NUL", "..", "a:b"}
	invalid := []string{"app.toml.", "app.toml ", "sub/", `sub\`, "sub//x", "sub/../app.toml", "./app.toml", "sub./x", "sub /x"}
	for _, name := range append(append([]string(nil), escapes...), invalid...) {
		for _, opts := range [][]Option{nil, {NoSync()}, {KeepMode()}, {IfHolds(old)}, {NoReplace()}} {
			err := FileIn(root, name, []byte("new"), opts...)
			if err == nil || errors.Is(err, ErrChanged) {
				t.Errorf("FileIn(%q) with %d options: %v, and want it refused before any open", name, len(opts), err)
			}
			if slices.Contains(invalid, name) && !errors.Is(err, fs.ErrInvalid) {
				t.Errorf("FileIn(%q) with %d options: %v, and want fs.ErrInvalid", name, len(opts), err)
			}
		}
	}
	for _, base := range []string{"app.toml.", "app.toml "} {
		for _, opts := range [][]Option{nil, {NoSync()}} {
			if err := File(filepath.Join(dir, base), []byte("new"), opts...); !errors.Is(err, fs.ErrInvalid) {
				t.Errorf("File(%q) with %d options: %v, and want fs.ErrInvalid", base, len(opts), err)
			}
		}
	}
	holds(t, path, "old")
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm()&0o200 != 0 {
		t.Errorf("the old file is %v (%v), and want it read-only still", fi, err)
	}
	alone(t, dir, "app.toml", "sub")
	alone(t, filepath.Join(dir, "sub"))
}

// A full log is not moved aside to its own name in a spelling that differs from it as written but
// names it to os.Root: with a trailing dot or space, which the Win32 name rules strip, or in
// another case, which NTFS ignores. The move is refused, the log keeps its lines, and no line is
// written.
//
// What it costs when this fails: the log is renamed over itself, or to its own name in another
// case, and Append goes round its tries on a log it cannot move, then fails with no line written
// and the log's name changed.
func TestALogIsNotMovedAsideToItsOwnNameSpelledAnotherWay(t *testing.T) {
	t.Parallel()
	for _, other := range []string{"c.log.", "c.log ", "C.LOG"} {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		put0(t, filepath.Join(dir, "c.log"), "full")
		if err := Append(t.Context(), root, "c.log", []byte("line"), 5, Aside(other)); err == nil {
			t.Errorf("Aside(%q): a full log was moved aside to its own name", other)
		}
		holds(t, filepath.Join(dir, "c.log"), "full")
		names, err := os.ReadDir(dir)
		if err != nil || len(names) != 1 || names[0].Name() != "c.log" {
			t.Errorf("Aside(%q): %s holds %v (%v), and want c.log alone, in its case", other, dir, names, err)
		}
		if err := root.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// KeepMode over an old file whose list grants nothing beyond this account, SYSTEM and
// Administrators, a denial to Guests among it, is taken in File and FileIn: the new file has the
// owner-only list and keeps the old file's read-only attribute. With no file at the name it is
// taken. Over an old file whose list grants Users or Everyone read too, has a NULL list, which
// lets every account in, or has a callback or an object entry, which Narrow does not read, it is
// refused by heldBy with ErrNoMode, matching
// errors.ErrUnsupported, in File, FileIn and PutBack, before anything is made, and the old file
// stays. Keep, which replaces no file, refuses it with ErrNoMode. Every list is one
// this account may read: a denial to a group it is in, as Users, would refuse the look itself.
//
// What it costs when this fails: a shared file a program rewrites with KeepMode, so the accounts
// that read it still can, is locked to the writer, and a service that reads it fails; or a
// read-only file comes back writable.
func TestKeepModeIsTakenOnlyOverAFileTheOwnerOnlyListCovers(t *testing.T) {
	t.Parallel()
	refused := func(t *testing.T, what string, err error) {
		t.Helper()
		if !errors.Is(err, ErrNoMode) || !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("%s with KeepMode on Windows: %v, and want ErrNoMode matching errors.ErrUnsupported", what, err)
		}
	}
	mine := "(A;;FA;;;SY)(A;;FA;;;" + me(t) + ")"
	for _, c := range []struct {
		name     string
		list     string
		readOnly bool
	}{
		{"owner-only", "D:P" + mine, false},
		{"owner-only read-only", "D:P" + mine, true},
		{"and Administrators", "D:P" + mine + "(A;;FA;;;BA)", false},
		{"and a denial to Guests", "D:P(D;;FA;;;BG)" + mine, false},
	} {
		t.Run("taken "+c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "app.toml")
			if err := File(path, []byte("new"), KeepMode(), NoSync()); err != nil {
				t.Fatalf("File with KeepMode and no file there: %v", err)
			}
			holds(t, path, "new")
			ownerOnly(t, path)
			for i, step := range []func() error{
				func() error { return File(path, []byte("newer"), KeepMode(), NoSync()) },
				func() error {
					root, err := os.OpenRoot(dir)
					if err != nil {
						return err
					}
					return errors.Join(FileIn(root, "app.toml", []byte("newer"), KeepMode(), NoSync()), root.Close())
				},
			} {
				put0(t, path, "old")
				listed(t, path, c.list)
				if c.readOnly {
					if err := os.Chmod(path, 0o444); err != nil {
						t.Fatal(err)
					}
				}
				if err := step(); err != nil {
					t.Fatalf("write %d with KeepMode over a file listed %s: %v", i, c.list, err)
				}
				holds(t, path, "newer")
				ownerOnly(t, path)
				if got := isReadOnly(t, path); got != c.readOnly {
					t.Errorf("write %d with KeepMode over a file listed %s, read-only %v: read-only %v", i, c.list, c.readOnly, got)
				}
				if err := os.Chmod(path, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			alone(t, dir)
		})
	}
	for _, c := range []struct {
		name, list string
		object     bool
	}{
		{"Users read", "D:P" + mine + "(A;;FR;;;BU)", false},
		{"Everyone read", "D:P" + mine + "(A;;FR;;;WD)", false},
		{"NULL", "D:NO_ACCESS_CONTROL", false},
		{"a callback entry", "D:P" + mine + "(XA;;FR;;;WD;(Member_of {SID(BA)}))", false},
		{"an object entry", "D:P" + mine + "(OA;;FR;5f9e6a10-0b7c-4d3e-9a41-2c8d7e6f1b30;;WD)", true},
	} {
		t.Run("refused "+c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "app.toml")
			put0(t, path, "old")
			if err := setList(path, c.list); err != nil && c.object {
				// No file here can have the list, so none reaches heldBy with it.
				t.Logf("this volume refuses an object entry, so no old file has one: %v", err)
				holds(t, path, "old")
				return
			} else if err != nil {
				t.Fatal(err)
			} else if c.object {
				t.Logf("this volume takes an object entry")
			}
			refused(t, "File over a file listed "+c.list, File(path, []byte("new"), KeepMode(), NoSync()))
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			refused(t, "FileIn over a file listed "+c.list, FileIn(root, "app.toml", []byte("new"), KeepMode(), NoSync()))
			if err := root.Close(); err != nil {
				t.Fatal(err)
			}
			holds(t, path, "old")
			alone(t, dir, "app.toml")
			kept, err := Keep(path, dir, "app.toml.kept", NoSync())
			if err != nil {
				t.Fatal(err)
			}
			if err := File(path, []byte("after"), NoSync()); err != nil {
				t.Fatal(err)
			}
			listed(t, path, c.list)
			refused(t, "PutBack over a file listed "+c.list, PutBack(kept, path, sha256.Sum256([]byte("after")), KeepMode(), NoSync()))
			holds(t, path, "after")
			alone(t, dir, "app.toml", filepath.Base(kept))
		})
	}
	t.Run("Keep refuses it", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "app.toml")
		if err := File(path, []byte("old"), NoSync()); err != nil {
			t.Fatal(err)
		}
		_, err := Keep(path, dir, "app.toml.kept", KeepMode(), NoSync())
		refused(t, "Keep", err)
		alone(t, dir, "app.toml")
	})
}

// KeepOwner over an old file this account owns, as a file write makes it, is taken in File, FileIn
// and PutBack, and the new file is this account's with the owner-only list; with no file at the
// name it is taken. Over an old file another account owns, it is refused with ErrNoOwner, matching
// errors.ErrUnsupported, before anything is made, and the old file stays. A plain token cannot make
// a file another account owns, so the test takes this account as another (asAnother, read only
// under testing.Testing()); an elevated run also refuses a plain os.WriteFile's file, which
// Administrators owns. Keep, which replaces no file, refuses it with ErrNoOwner.
//
// What it costs when this fails: a program that rewrites a service's file with KeepOwner, so the
// service still owns it, takes it from the service, which can no longer change its list.
func TestKeepOwnerIsTakenOnlyOverAFileThisAccountOwns(t *testing.T) {
	t.Parallel()
	refused := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, ErrNoOwner) || !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("%s with KeepOwner on Windows: %v, and want ErrNoOwner matching errors.ErrUnsupported", what, err)
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	if err := File(path, []byte("old"), KeepOwner(), NoSync()); err != nil {
		t.Fatalf("File with KeepOwner and no file there: %v", err)
	}
	holds(t, path, "old")
	ownerOnly(t, path)
	refused("File over another account's file", File(path, []byte("new"), KeepOwner(), asAnother(), NoSync()))
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	refused("FileIn over another account's file", FileIn(root, "app.toml", []byte("new"), KeepOwner(), asAnother(), NoSync()))
	holds(t, path, "old")
	alone(t, dir, "app.toml")
	if err := File(path, []byte("new"), KeepOwner(), NoSync()); err != nil {
		t.Fatalf("File with KeepOwner over this account's file: %v", err)
	}
	holds(t, path, "new")
	ownerOnly(t, path)
	if err := FileIn(root, "app.toml", []byte("newer"), KeepOwner(), NoSync()); err != nil {
		t.Fatalf("FileIn with KeepOwner over this account's file: %v", err)
	}
	holds(t, path, "newer")
	ownerOnly(t, path)
	_, err = Keep(path, dir, "app.toml.kept", KeepOwner(), NoSync())
	refused("Keep", err)
	alone(t, dir, "app.toml")
	kept, err := Keep(path, dir, "app.toml.kept", NoSync())
	if err != nil {
		t.Fatal(err)
	}
	if err := File(path, []byte("after"), NoSync()); err != nil {
		t.Fatal(err)
	}
	refused("PutBack over another account's file", PutBack(kept, path, sha256.Sum256([]byte("after")), KeepOwner(), asAnother(), NoSync()))
	holds(t, path, "after")
	if err := PutBack(kept, path, sha256.Sum256([]byte("after")), KeepOwner(), NoSync()); err != nil {
		t.Fatalf("PutBack with KeepOwner over this account's file: %v", err)
	}
	holds(t, path, "newer")
	ownerOnly(t, path)
	alone(t, dir, "app.toml", filepath.Base(kept))
	plain := filepath.Join(dir, "plain.toml")
	put0(t, plain, "plain")
	if owner, _, _ := listOf(t, plain); owner != me(t) {
		refused("File over "+owner+"'s file", File(plain, []byte("new"), KeepOwner(), NoSync()))
		holds(t, plain, "plain")
	}
}

// A file swapped in at the name between kept's look at the old file and heldBy's open of it is
// refused, under KeepMode and under KeepOwner, in File and FileIn, and nothing is written: the
// list Narrow reads and the owner Mine reads are the looked-at file's, or none are read. The file
// swapped in is this account's, with its folder's list, which Narrow and Mine pass, so only the
// match with the look refuses it.
//
// What it costs when this fails: the list and owner judged are one file's, and the mode and owner
// kept are another's, so a swap after the look keeps what was never judged.
func TestAFileSwappedInAfterKeptsLookIsRefused(t *testing.T) {
	t.Parallel()
	for _, keep := range []struct {
		name string
		opt  Option
	}{{"KeepMode", KeepMode()}, {"KeepOwner", KeepOwner()}} {
		for _, in := range []string{"File", "FileIn"} {
			t.Run(keep.name+" "+in, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				path := filepath.Join(dir, "app.toml")
				put0(t, path, "old")
				swap := stepWith(func(at string) {
					if at != "kept" {
						return
					}
					other := filepath.Join(dir, "other")
					put0(t, other, "theirs")
					if err := os.Rename(other, path); err != nil {
						t.Errorf("swap: %v", err)
					}
				})
				var err error
				if in == "File" {
					err = File(path, []byte("new"), keep.opt, swap, NoSync())
				} else {
					root, open := os.OpenRoot(dir)
					if open != nil {
						t.Fatal(open)
					}
					err = errors.Join(FileIn(root, "app.toml", []byte("new"), keep.opt, swap, NoSync()), root.Close())
				}
				if err == nil || !strings.Contains(err.Error(), "changed between its look and its open, and nothing was written") {
					t.Errorf("%s with %s and a file swapped in after the look: %v, and want it refused as changed between its look and its open", in, keep.name, err)
				}
				holds(t, path, "theirs")
				alone(t, dir, "app.toml")
			})
		}
	}
}

// fileInternalInformation is FILE_INFORMATION_CLASS's FileInternalInformation, which x/sys does not
// name: a handle's file index, answered whatever rights the handle has.
const fileInternalInformation = 6

// SyncFolder opens the folder again to write and flushes it, by FlushFileBuffers on a handle to
// that folder: once for each call, the flush answering nil on NTFS. A folder another process holds
// sharing no write, so it cannot be opened to write, is that open's error: SyncFolder does more
// than look.
//
// What it costs when this fails: a program flushes a folder after a removal or an exclusive
// create, is told it did, and a power cut brings the old folder back.
func TestSyncFolderFlushesTheFolderOpenToWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "a"), 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	d, err := root.Open("a")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	var want windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(d.Fd()), &want); err != nil {
		t.Fatal(err)
	}
	index := uint64(want.FileIndexHigh)<<32 | uint64(want.FileIndexLow)
	flushed := 0
	err = flushSeen(d, func(h windows.Handle, err error) error {
		flushed++
		// The handle is open to write alone, as the flush needs no more, so GetFileInformationByHandle
		// is denied on it. FileInternalInformation needs no right on the handle: its index is the
		// folder's, on d's volume, as the handle is opened relative to d.
		var id [8]byte
		var iosb windows.IO_STATUS_BLOCK
		gerr := windows.NtQueryInformationFile(h, &iosb, &id[0], uint32(len(id)), fileInternalInformation)
		if got := binary.LittleEndian.Uint64(id[:]); gerr != nil || got != index {
			t.Errorf("the handle flushed is not the folder's: index %#x (%v), and want %#x", got, gerr, index)
		}
		return err
	})
	if err != nil || flushed != 1 {
		t.Fatalf("the folder flushed %d times, with %v, and want once with nil", flushed, err)
	}

	p, err := windows.UTF16PtrFromString(filepath.Join(dir, "a"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(h) }()
	if err := SyncFolder(root, "a"); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Errorf("SyncFolder of a folder held sharing no write: %v, and want the sharing violation of its open to write", err)
	}
}

// A flush the volume will not do matches errors.ErrUnsupported, as FAT may answer one; a flush
// refused otherwise, as access denied, is its own error and not that. Neither answers nil.
//
// What it costs when this fails: a program on a volume that cannot flush a folder is told the
// folder was flushed, or a denial is taken as a volume that cannot, and passed over.
func TestARefusedFolderFlushIsNeverNil(t *testing.T) {
	t.Parallel()
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	d, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	for _, refusal := range []error{windows.ERROR_INVALID_FUNCTION, windows.ERROR_NOT_SUPPORTED, windows.ERROR_INVALID_PARAMETER} {
		err := flushSeen(d, func(windows.Handle, error) error { return refusal })
		if !errors.Is(err, errors.ErrUnsupported) || !errors.Is(err, refusal) {
			t.Errorf("a flush refused with %v: %v, and want it matching errors.ErrUnsupported and itself", refusal, err)
		}
	}
	err = flushSeen(d, func(windows.Handle, error) error { return windows.ERROR_ACCESS_DENIED })
	if errors.Is(err, errors.ErrUnsupported) || !errors.Is(err, fs.ErrPermission) {
		t.Errorf("a flush denied: %v, and want fs.ErrPermission, not errors.ErrUnsupported", err)
	}
}

// A new log Append makes is open with the rights a log there already is reopened with, the rights
// os gives O_RDWR|O_APPEND: read, append and its attributes, and never FILE_WRITE_DATA, so each
// write goes to the end whichever log it is.
//
// What it costs when this fails: a call on Append's handle that sets the log's attributes or
// times works on a log reopened and fails on a log just made, so the first line of a log and every
// later one are written through handles that differ in what they may do.
func TestANewLogIsOpenWithTheRightsOfALogReopened(t *testing.T) {
	t.Parallel()
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	made, err := openLog(root, "a.log", settle(nil))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = made.Close() }()
	again, err := openLog(root, "a.log", settle(nil))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = again.Close() }()
	m, a := granted(t, made), granted(t, again)
	if m != a {
		t.Errorf("a new log is open with access %#x, and a log reopened with %#x", m, a)
	}
	if m&windows.FILE_WRITE_DATA != 0 || m&windows.FILE_APPEND_DATA == 0 {
		t.Errorf("a new log is open with access %#x, and want FILE_APPEND_DATA without FILE_WRITE_DATA", m)
	}
}

// fileAccessInformation is FILE_INFORMATION_CLASS's FileAccessInformation: a handle's granted access.
const fileAccessInformation = 8

// granted is the access f's handle was given, as the system says it.
func granted(t *testing.T, f *os.File) uint32 {
	t.Helper()
	var b [4]byte
	var iosb windows.IO_STATUS_BLOCK
	if err := windows.NtQueryInformationFile(windows.Handle(f.Fd()), &iosb, &b[0], uint32(len(b)), fileAccessInformation); err != nil {
		t.Fatal(err)
	}
	return binary.LittleEndian.Uint32(b[:])
}

// KeepMode and KeepOwner over a name that is neither a plain file nor a link are refused, with
// ErrNoMode and ErrNoOwner, before anything is made: a junction to a folder beside it, and a file
// carrying a reparse point of a driver's own tag, which Go reads as irregular. The name is left as
// it was, and nothing else is in the folder.
//
// What it costs when this fails: a write that keeps the old file's mode or owner passes over a
// name it never read either from, and replaces a junction or a driver's file with one that keeps
// neither.
func TestKeepModeAndKeepOwnerOverAnIrregularNameAreRefused(t *testing.T) {
	t.Parallel()
	for _, keep := range []struct {
		name string
		opt  Option
		want error
	}{{"KeepMode", KeepMode(), ErrNoMode}, {"KeepOwner", KeepOwner(), ErrNoOwner}} {
		for _, kind := range []string{"junction", "tagged"} {
			t.Run(keep.name+" "+kind, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				path := filepath.Join(dir, "app.toml")
				names := []string{"app.toml"}
				if kind == "junction" {
					target := filepath.Join(dir, "target")
					if err := os.Mkdir(target, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(path, 0o700); err != nil {
						t.Fatal(err)
					}
					reparse(t, path, junctionData(target))
					names = append(names, "target")
				} else {
					put0(t, path, "old")
					reparse(t, path, taggedData())
				}
				fi, err := os.Lstat(path)
				if err != nil || fi.Mode().Type() != fs.ModeIrregular && fi.Mode().Type() != fs.ModeIrregular|fs.ModeDir {
					t.Fatalf("the %s name reads as %v (%v), and want it irregular", kind, fi, err)
				}
				if err := File(path, []byte("new"), keep.opt, NoSync()); !errors.Is(err, keep.want) {
					t.Errorf("File with %s over a %s name: %v, and want %v", keep.name, kind, err, keep.want)
				}
				if after, err := os.Lstat(path); err != nil || after.Mode().Type() != fi.Mode().Type() {
					t.Errorf("the %s name reads as %v after the refusal (%v), and want it left as %v", kind, after, err, fi.Mode().Type())
				}
				alone(t, dir, names...)
			})
		}
	}
}

// reparse sets the reparse point data on the file or folder at path, with FSCTL_SET_REPARSE_POINT,
// which the account that owns it may set with no privilege for any tag but a symbolic link's.
func reparse(t *testing.T, path string, data []byte) {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	var n uint32
	err = windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &data[0], uint32(len(data)), nil, 0, &n, nil)
	if cerr := windows.CloseHandle(h); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatalf("set a reparse point on %s: %v", path, err)
	}
}

// junctionData is a mount point's reparse data, leading to the folder target.
func junctionData(target string) []byte {
	substitute, shown := utf16.Encode([]rune(`\??\`+target)), utf16.Encode([]rune(target))
	names := make([]uint16, 0, len(substitute)+len(shown)+2)
	names = append(append(append(append(names, substitute...), 0), shown...), 0)
	b := binary.LittleEndian.AppendUint32(nil, windows.IO_REPARSE_TAG_MOUNT_POINT)
	for _, v := range []int{8 + 2*len(names), 0, 0, 2 * len(substitute), 2 * (len(substitute) + 1), 2 * len(shown)} {
		b = binary.LittleEndian.AppendUint16(b, uint16(v))
	}
	for _, c := range names {
		b = binary.LittleEndian.AppendUint16(b, c)
	}
	return b
}

// taggedData is the reparse data of a tag no Microsoft driver owns, 0x00000BAD, in the form a
// driver's tag takes: the tag, the length of its own data, a GUID, and four bytes of data.
func taggedData() []byte {
	b := binary.LittleEndian.AppendUint32(nil, 0x00000BAD)
	b = binary.LittleEndian.AppendUint16(b, 4)
	b = binary.LittleEndian.AppendUint16(b, 0) // reserved
	b = append(b, 0x6f, 0x1b, 0x2c, 0x3d, 0x4e, 0x5f, 0x60, 0x71, 0x82, 0x93, 0xa4, 0xb5, 0xc6, 0xd7, 0xe8, 0xf9)
	return append(b, 'k', 'i', 'l', 'n')
}

// Keep's copy keeps the read-only attribute of the file it copies, and PutBack brings kept's back,
// with a Mode given or not, as Unix gives both their source's mode: a read-only file's copy and
// restore are read-only, and a writable file's are writable.
//
// What it costs when this fails: a config a person or an installer marked read-only is writable
// after a restore, so a stray write it was meant to refuse goes through.
func TestKeepAndPutBackKeepTheReadOnlyAttribute(t *testing.T) {
	t.Parallel()
	for _, source := range []struct {
		name     string
		readOnly bool
	}{{"read-only", true}, {"writable", false}} {
		for _, given := range []struct {
			name string
			opts []Option
		}{{"no Mode", []Option{NoSync()}}, {"Mode(0o600)", []Option{Mode(0o600), NoSync()}}} {
			t.Run(source.name+" "+given.name, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				path := filepath.Join(dir, "app.toml")
				put0(t, path, "old")
				if source.readOnly {
					if err := os.Chmod(path, 0o400); err != nil {
						t.Fatal(err)
					}
				}
				kept, err := Keep(path, dir, "app.toml.kept", given.opts...)
				if err != nil {
					t.Fatalf("Keep with %s: %v", given.name, err)
				}
				if got := isReadOnly(t, kept); got != source.readOnly {
					t.Errorf("Keep with %s of a %s file: the copy is read-only %v, and want %v", given.name, source.name, got, source.readOnly)
				}
				holds(t, kept, "old")
				if err := File(path, []byte("after"), NoSync()); err != nil {
					t.Fatal(err)
				}
				if isReadOnly(t, path) {
					t.Fatal("File left the file it replaced read-only")
				}
				if err := PutBack(kept, path, sha256.Sum256([]byte("after")), given.opts...); err != nil {
					t.Fatalf("PutBack with %s: %v", given.name, err)
				}
				if got := isReadOnly(t, path); got != source.readOnly {
					t.Errorf("PutBack with %s of a copy of a %s file: the file is read-only %v, and want %v", given.name, source.name, got, source.readOnly)
				}
				holds(t, path, "old")
				ownerOnly(t, path)
				alone(t, dir, "app.toml", "app.toml.kept")
			})
		}
	}
}

// A NoReplace write with a Mode without owner write leaves the placed name read-only, by File and
// by FileIn, with the attribute as the temp file had it and with it taken off once the file is in
// place, as Go's delete of the temp name takes it off the file both names share where a volume will
// not delete a read-only file as is. NTFS deletes it as is, so the step
// seam takes the attribute off.
//
// What it costs when this fails: a file written read-only by NoReplace is writable, and the write
// says nothing.
func TestANoReplaceWriteWithAReadOnlyModeLeavesThePlacedNameReadOnly(t *testing.T) {
	t.Parallel()
	for _, cleared := range []bool{false, true} {
		for _, in := range []string{"File", "FileIn"} {
			t.Run(in+map[bool]string{false: "", true: " with the attribute taken off once placed"}[cleared], func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				path := filepath.Join(dir, "app.toml")
				opts := []Option{Mode(0o444), NoReplace(), NoSync()}
				if cleared {
					opts = append(opts, stepWith(func(at string) {
						if at == "placed" {
							if err := os.Chmod(path, 0o600); err != nil {
								t.Errorf("take the attribute off: %v", err)
							}
						}
					}))
				}
				var err error
				if in == "File" {
					err = File(path, []byte("new"), opts...)
				} else {
					root, open := os.OpenRoot(dir)
					if open != nil {
						t.Fatal(open)
					}
					err = errors.Join(FileIn(root, "app.toml", []byte("new"), opts...), root.Close())
				}
				if err != nil {
					t.Fatalf("%s with Mode(0o444) and NoReplace: %v", in, err)
				}
				if !isReadOnly(t, path) {
					t.Errorf("%s with Mode(0o444) and NoReplace left the placed name writable", in)
				}
				holds(t, path, "new")
				ownerOnly(t, path)
				alone(t, dir, "app.toml")
			})
		}
	}
}

// A NoReplace write with a Mode without owner write, whose placed name is taken by another file of
// this account's once it is in place, writable, is refused, by File and by FileIn, and the other
// file is left at the name as it was, writable and holding its own bytes: stays reads the name it
// sets the attribute on and holds it to the file written.
//
// What it costs when this fails: a write sets the read-only attribute on a file it did not write,
// one another program put at the name, and says the write is done.
func TestANoReplaceWriteRefusesAPlacedNameSwappedOnceInPlace(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"File", "FileIn"} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "app.toml")
			other := filepath.Join(dir, "other")
			swapped := false
			opts := []Option{Mode(0o444), NoReplace(), NoSync(), stepWith(func(at string) {
				if at != "placed" || swapped {
					return
				}
				swapped = true
				if err := os.Chmod(path, 0o600); err != nil {
					t.Errorf("take the attribute off: %v", err)
				}
				if err := os.WriteFile(other, []byte("theirs"), 0o600); err != nil {
					t.Errorf("write the other file: %v", err)
				}
				if err := os.Rename(other, path); err != nil {
					t.Errorf("rename the other file over the placed name: %v", err)
				}
			})}
			var err error
			if in == "File" {
				err = File(path, []byte("new"), opts...)
			} else {
				root, open := os.OpenRoot(dir)
				if open != nil {
					t.Fatal(open)
				}
				err = errors.Join(FileIn(root, "app.toml", []byte("new"), opts...), root.Close())
			}
			if !swapped {
				t.Fatalf("%s never reached the placed step", in)
			}
			if err == nil {
				t.Errorf("%s with Mode(0o444) and NoReplace, the placed name swapped once in place: <nil>, and want it refused as swapped", in)
			}
			if isReadOnly(t, path) {
				t.Errorf("%s set the read-only attribute on the file swapped in, and want it left writable", in)
			}
			holds(t, path, "theirs")
			alone(t, dir, "app.toml")
		})
	}
}

// A file put at the temp name after the write's last check is never renamed over the name: the
// rename goes by the handle opened at the temp name while the write still held the file, and
// compared with it, so the handle holds the written file's record to the rename. The temp file
// removed at the written step leaves that handle on the file written, so the write either places
// its own file or is refused with the old file left; the name never holds the other one. That
// holds with NoSync, and on a volume that refuses write-through, as with neither. Not shown: NTFS
// handing the removed file's record out again, which the open handle forbids.
//
// What it costs when this fails: an account that can write the folder removes the temp file after
// the last check and puts its own at the name, and the write puts that file in place of the old one
// as if it were the one written.
func TestAFilePutAtTheTempNameAfterTheLastCheckIsNeverRenamed(t *testing.T) {
	t.Parallel()
	ways := map[string][]Option{
		"":                nil,
		"NoSync":          {NoSync()},
		"through refused": {throughWith(func(error) error { return windows.STATUS_INVALID_PARAMETER })},
	}
	for _, in := range []string{"File", "FileIn"} {
		for way, with := range ways {
			t.Run(in+" "+way, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				path := filepath.Join(dir, "app.toml")
				put0(t, path, "old")
				swapped := false
				var put error // a name the write's handle still holds may refuse the put
				opts := []Option{stepWith(func(at string) {
					if at != "written" || swapped {
						return // "written" is past the last check and before the rename
					}
					swapped = true
					temps, err := filepath.Glob(filepath.Join(dir, ".app.toml.*.tmp"))
					if err != nil || len(temps) != 1 {
						t.Errorf("the temp file is at %v (%v), and want one", temps, err)
						return
					}
					if err := os.Remove(temps[0]); err != nil {
						t.Errorf("remove the temp file: %v", err)
					}
					put = os.WriteFile(temps[0], []byte("theirs"), 0o600)
				})}
				opts = append(opts, with...)
				var err error
				if in == "File" {
					err = File(path, []byte("new"), opts...)
				} else {
					root, open := os.OpenRoot(dir)
					if open != nil {
						t.Fatal(open)
					}
					err = errors.Join(FileIn(root, "app.toml", []byte("new"), opts...), root.Close())
				}
				if !swapped {
					t.Fatalf("%s never reached the written step", in)
				}
				b, rerr := os.ReadFile(path)
				t.Logf("%s %s: the put at the temp name: %v; the write: %v; the name holds %q (%v)", in, way, put, err, b, rerr)
				switch {
				case rerr != nil || string(b) != "old" && string(b) != "new":
					t.Errorf("%s %s with another file put at the temp name after the last check: the name holds %q (%v), and want old or new, never that file", in, way, b, rerr)
				case string(b) == "old" && err == nil:
					t.Errorf("%s %s left the old file and said <nil>, and want it refused", in, way)
				}
				temps, _ := filepath.Glob(filepath.Join(dir, ".app.toml.*.tmp"))
				for _, tmp := range temps { // the other account's file, which the write leaves where it put it
					holds(t, tmp, "theirs")
					if err := os.Remove(tmp); err != nil {
						t.Error(err)
					}
				}
				alone(t, dir, "app.toml")
			})
		}
	}
}

// From the write's last check to the rename, the temp file is open by the rename's handle, which
// holds delete: an open that does not share delete is refused as a sharing violation at the written
// step, with NoSync and on a volume that refuses write-through too. The handle was opened and
// compared while the write still held the file, so no other file can take its record before the
// rename.
//
// What it costs when this fails: the rename's handle is opened after the write let the file go, so
// an account that removes the temp file and makes files until NTFS hands its record out again puts
// its own file at the temp name, and the compare by volume and index takes it as the one written.
func TestTheTempFileIsHeldFromTheLastCheckToTheRename(t *testing.T) {
	t.Parallel()
	ways := map[string][]Option{
		"":                nil,
		"NoSync":          {NoSync()},
		"through refused": {throughWith(func(error) error { return windows.STATUS_INVALID_PARAMETER })},
	}
	for way, with := range ways {
		t.Run(way, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "app.toml")
			put0(t, path, "old")
			reached := false
			opts := append([]Option{stepWith(func(at string) {
				if at != "written" || reached {
					return
				}
				reached = true
				temps, err := filepath.Glob(filepath.Join(dir, ".app.toml.*.tmp"))
				if err != nil || len(temps) != 1 {
					t.Errorf("the temp file is at %v (%v), and want one", temps, err)
					return
				}
				name, err := windows.UTF16PtrFromString(temps[0])
				if err != nil {
					t.Error(err)
					return
				}
				h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
					windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
				if err == nil {
					_ = windows.CloseHandle(h)
				}
				if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
					t.Errorf("%q: an open of the temp file that does not share delete, at the written step: %v, and want ERROR_SHARING_VIOLATION, as the rename's handle holds it", way, err)
				}
			})}, with...)
			err := File(path, []byte("new"), opts...)
			if !reached {
				t.Fatal("the write never reached the written step")
			}
			if err != nil && !errors.Is(err, ErrNotDurable) {
				t.Fatalf("%q: %v", way, err)
			}
			holds(t, path, "new")
			alone(t, dir, "app.toml")
		})
	}
}

// A file put at the temp name while the write still holds the file, and before the rename's handle
// is opened at it, is refused as swapped, with the old file kept at the name: the handle is
// compared with the file written as it is opened (gripTemp). The row puts the file there from the
// write-through seam, which refuses the first open as a volume without write-through does, so the
// open without it, the one the rename goes by, finds the other file; the compare is the same one
// on every path.
//
// What it costs when this fails: the handle the rename goes by is another account's file, which
// the write puts in place of the old one as if it were the one written.
func TestAFilePutAtTheTempNameBeforeTheGripIsRefused(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"File", "FileIn"} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "app.toml")
			put0(t, path, "old")
			put0(t, filepath.Join(dir, "theirs"), "theirs")
			swapped := false
			refuse := throughWith(func(error) error {
				if !swapped {
					swapped = true
					temps, err := filepath.Glob(filepath.Join(dir, ".app.toml.*.tmp"))
					if err != nil || len(temps) != 1 {
						t.Errorf("the temp file is at %v (%v), and want one", temps, err)
					} else if r, err := os.OpenRoot(dir); err != nil {
						t.Error(err)
					} else {
						// By the root: its rename has POSIX semantics, which the write's handles,
						// shared for delete, do not stop.
						if err := r.Rename("theirs", filepath.Base(temps[0])); err != nil {
							t.Errorf("rename another file over the temp name: %v", err)
						}
						_ = r.Close()
					}
				}
				return windows.STATUS_INVALID_PARAMETER
			})
			var err error
			if in == "File" {
				err = File(path, []byte("new"), refuse)
			} else {
				root, open := os.OpenRoot(dir)
				if open != nil {
					t.Fatal(open)
				}
				err = errors.Join(FileIn(root, "app.toml", []byte("new"), refuse), root.Close())
			}
			if !swapped {
				t.Fatalf("%s never opened the temp file to write the rename through", in)
			}
			if err == nil || !strings.Contains(err.Error(), "was swapped for another file while it was written") || errors.Is(err, ErrNotDurable) {
				t.Errorf("%s with another file put at the temp name before the rename's handle was opened: %v, and want it refused as swapped", in, err)
			}
			holds(t, path, "old")
			alone(t, dir, "app.toml")
		})
	}
}

// While the temp file is in its folder, the grip holds both open to the rename, and the folder
// cannot be moved: the move is ERROR_ACCESS_DENIED, and no other error stands for it.
//
// What it costs when this fails: a folder can be swapped under the rename, and the file is put
// where the name no longer leads.
func TestAFolderHoldingTheTempFileCannotBeMoved(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	put0(t, filepath.Join(dir, "sub", "app.toml"), "old")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	tried := false
	err = FileIn(root, filepath.Join("sub", "app.toml"), []byte("new"), stepWith(func(at string) {
		if at != "written" {
			return
		}
		tried = true
		if err := os.Rename(filepath.Join(dir, "sub"), filepath.Join(dir, "moved")); !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			t.Errorf("a folder holding the temp file moved, or not by ERROR_ACCESS_DENIED: %v", err)
		}
	}))
	if !tried {
		t.Fatal("the folder was never moved")
	}
	if err != nil {
		t.Fatalf("a write beside a folder that could not be moved: %v", err)
	}
	holds(t, filepath.Join(dir, "sub", "app.toml"), "new")
}

// A folder moved away after the temp file is taken out of it, and a plain folder made at its name,
// is where the file is put: the rename opens the folder beneath the root again, so it lands at the
// name, and not in the folder moved away.
//
// What it costs when this fails: a write reports success with nothing at its name, the file in a
// folder the name no longer leads to.
func TestAFolderMadeAtTheNameAfterTheTempFileLeftIsWhereTheFileGoes(t *testing.T) {
	t.Parallel()
	dir, out := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	put0(t, filepath.Join(dir, "sub", "app.toml"), "old")
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
		if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
			t.Fatal(err)
		}
		swapped = true
	}))
	if !swapped {
		t.Fatal("the folder was never swapped")
	}
	if err != nil {
		t.Fatalf("a folder made at the name: %v, and want the file put there", err)
	}
	holds(t, filepath.Join(dir, "sub", "app.toml"), "new")
	holds(t, filepath.Join(dir, "moved", "app.toml"), "old")
}

// swapTempFor puts the file theirs, which the test made in dir, at the one temp name in dir, by the
// root's rename, whose POSIX semantics the write's handles, shared for delete, do not stop.
func swapTempFor(t *testing.T, dir string) {
	t.Helper()
	temps, err := filepath.Glob(filepath.Join(dir, ".app.toml.*.tmp"))
	if err != nil || len(temps) != 1 {
		t.Errorf("the temp file is at %v (%v), and want one", temps, err)
		return
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Error(err)
		return
	}
	if err := r.Rename("theirs", filepath.Base(temps[0])); err != nil {
		t.Errorf("rename another file over the temp name: %v", err)
	}
	_ = r.Close()
}

// A file put at the temp name at the gripping step, between the compare of ready and the open that
// grips the temp file, is refused as swapped, with the old file kept, for File and FileIn, with
// NoSync, and on a volume that refuses write-through.
//
// What it costs when this fails: the open the rename goes by is another file's, in the one step
// the earlier compare no longer covers.
func TestAFilePutAtTheTempNameAtTheGrippingStepIsRefused(t *testing.T) {
	t.Parallel()
	ways := map[string][]Option{
		"":                nil,
		"NoSync":          {NoSync()},
		"through refused": {throughWith(func(error) error { return windows.STATUS_INVALID_PARAMETER })},
	}
	for _, in := range []string{"File", "FileIn"} {
		for way, with := range ways {
			t.Run(in+" "+way, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				path := filepath.Join(dir, "app.toml")
				put0(t, path, "old")
				put0(t, filepath.Join(dir, "theirs"), "theirs")
				reached := false
				opts := append([]Option{stepWith(func(at string) {
					if at == "gripping" && !reached {
						reached = true
						swapTempFor(t, dir)
					}
				})}, with...)
				var err error
				if in == "File" {
					err = File(path, []byte("new"), opts...)
				} else {
					root, open := os.OpenRoot(dir)
					if open != nil {
						t.Fatal(open)
					}
					err = errors.Join(FileIn(root, "app.toml", []byte("new"), opts...), root.Close())
				}
				if !reached {
					t.Fatal("the write never reached the gripping step")
				}
				if err == nil || !strings.Contains(err.Error(), "was swapped for another file while it was written") {
					t.Errorf("%s %q with another file put at the temp name at the gripping step: %v, and want it refused as swapped", in, way, err)
				}
				holds(t, path, "old")
				alone(t, dir, "app.toml")
			})
		}
	}
}

// A file put at the temp name after the last check is refused under NoReplace, and nothing is at
// the name: the link goes by the grip, the handle on the file written, and NTFS refuses it once the
// swap took the file's last name (Access is denied), so no file of the account that swapped is
// linked, and none is taken away. The error says the temp file was swapped. The row also has that
// account hold its file open without FILE_SHARE_DELETE, which refuses a remove of the temp name: it
// is left, and the name is still missing. The taking step is never reached: linked takes away only
// what a link put at the name, so reaching it means another account's file was there, for a moment,
// as a link by the temp name leaves it.
//
// What it costs when this fails: a process that waits for the name opens another account's file at
// it, or the write answers a link error that reads as anything but a swap.
func TestAFilePutAtTheTempNameAfterTheLastCheckIsRefusedAndNothingIsAtTheNameUnderNoReplace(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"File", "FileIn"} {
		for _, held := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s held %v", in, held), func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				path := filepath.Join(dir, "app.toml")
				put0(t, filepath.Join(dir, "theirs"), "theirs")
				reached := false
				var theirs windows.Handle
				opts := []Option{NoReplace(), stepWith(func(at string) {
					if at == "taking" {
						t.Error("linked reached its taking step: another account's file was linked at the name, and is taken away only after")
					}
					if at != "written" || reached {
						return
					}
					reached = true
					swapTempFor(t, dir)
					if !held {
						return
					}
					temps, err := filepath.Glob(filepath.Join(dir, ".app.toml.*.tmp"))
					if err != nil || len(temps) != 1 {
						t.Errorf("the temp file is at %v (%v), and want one", temps, err)
						return
					}
					name, err := windows.UTF16PtrFromString(temps[0])
					if err != nil {
						t.Error(err)
						return
					}
					if theirs, err = windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
						windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0); err != nil {
						t.Errorf("hold the swapped file without FILE_SHARE_DELETE: %v", err)
					}
				})}
				var err error
				if in == "File" {
					err = File(path, []byte("new"), opts...)
				} else {
					root, open := os.OpenRoot(dir)
					if open != nil {
						t.Fatal(open)
					}
					err = errors.Join(FileIn(root, "app.toml", []byte("new"), opts...), root.Close())
				}
				if theirs != 0 {
					_ = windows.CloseHandle(theirs)
				}
				if !reached {
					t.Fatal("the write never reached the written step")
				}
				if err == nil || !strings.Contains(err.Error(), "was swapped for another file while it was written") {
					t.Errorf("%s under NoReplace with another file put at the temp name: %v, and want it refused as swapped", in, err)
				}
				if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("the name after a refused NoReplace write: %v, and want it missing", err)
				}
				if !held {
					alone(t, dir)
				}
			})
		}
	}
}

// A temp name that cannot be removed once the file is linked under NoReplace still reaches the
// placed step and runs `stays`: a mode without owner write leaves the placed name read-only, though
// the attribute was taken off at the placed step. Returning place's error from put before stays
// turns this red.
//
// What it costs when this fails: a failed remove of the temp name returns before the read-only
// attribute is set again, and the placed name is writable with the mode asked for 0o400.
func TestATempNameThatCannotBeRemovedUnderNoReplaceStillLeavesThePlacedNameReadOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	denied := func(*os.Root, string) error { return fs.ErrPermission }
	placed := false
	err := File(path, []byte("new"), NoReplace(), Mode(0o400), removeWith(denied), stepWith(func(at string) {
		if at == "placed" {
			placed = true
			if err := os.Chmod(path, 0o600); err != nil {
				t.Errorf("take the attribute off: %v", err)
			}
		}
	}))
	if !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "its temp file is left") {
		t.Errorf("a failed remove of the temp name under NoReplace: %v, and want it to wrap the remove's and say the temp file is left", err)
	}
	if !placed {
		t.Error("a failed remove of the temp name after the file is placed never reached the placed step")
	}
	if !isReadOnly(t, path) {
		t.Error("the placed name is writable after a failed remove of the temp name, and want it read-only")
	}
}

// A grip that fails to close once the file is placed under NoReplace still reaches the placed step
// and runs `stays`: a mode without owner write leaves the placed name read-only, as it does when the
// close is whole, though the attribute was taken off at the placed step, as the remove of the temp
// name can take it off.
//
// What it costs when this fails: a failed close returns before the placed step or before the
// read-only attribute is set again, and the placed name is writable with the mode asked for 0o400.
func TestAGripThatFailsToCloseUnderNoReplaceStillLeavesThePlacedNameReadOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	boom := errors.New("the close failed")
	placed := false
	err := File(path, []byte("new"), NoReplace(), Mode(0o400), closeWith(func(error) error { return boom }), stepWith(func(at string) {
		if at == "placed" {
			placed = true
			if err := os.Chmod(path, 0o600); err != nil {
				t.Errorf("take the attribute off: %v", err)
			}
		}
	}))
	if !errors.Is(err, boom) {
		t.Errorf("a failed close under NoReplace: %v, and want it to wrap the close's", err)
	}
	if !placed {
		t.Error("a failed close after the file is placed never reached the placed step")
	}
	if !isReadOnly(t, path) {
		t.Error("the placed name is writable after a failed close, and want it read-only")
	}
}

// A swap that leaves the file written a name, the temp name renamed aside and another file renamed
// to the temp name, does not refuse NoReplace: the link by the grip succeeds, the file written is
// at the name, the other file is removed from the temp name (or left, and said to be, where its
// account holds it without FILE_SHARE_DELETE), and the file written keeps the name it was renamed
// to. Nothing of the other account's is at the name, at the placed step or after.
//
// What it costs when this fails: the other account's file is placed at a name the write was to
// make afresh.
func TestASwapThatLeavesTheFileWrittenANameStillPlacesTheFileWrittenUnderNoReplace(t *testing.T) {
	t.Parallel()
	for _, held := range []bool{false, true} {
		t.Run(fmt.Sprintf("held %v", held), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "app.toml")
			put0(t, filepath.Join(dir, "theirs"), "theirs")
			var theirs windows.Handle
			var temp string
			flushed := 0
			count := syncWith(func(*os.Root, string) error { flushed++; return nil })
			err := File(path, []byte("new"), NoReplace(), count, stepWith(func(at string) {
				switch at {
				case "written":
					temps, err := filepath.Glob(filepath.Join(dir, ".app.toml.*.tmp"))
					if err != nil || len(temps) != 1 {
						t.Errorf("the temp file is at %v (%v), and want one", temps, err)
						return
					}
					temp = filepath.Base(temps[0])
					r, err := os.OpenRoot(dir)
					if err != nil {
						t.Error(err)
						return
					}
					defer r.Close()
					if err := r.Rename(filepath.Base(temps[0]), "aside"); err != nil {
						t.Errorf("rename the temp file aside: %v", err)
					}
					if err := r.Rename("theirs", filepath.Base(temps[0])); err != nil {
						t.Errorf("rename another file to the temp name: %v", err)
					}
					if held {
						name, err := windows.UTF16PtrFromString(temps[0])
						if err != nil {
							t.Error(err)
							return
						}
						if theirs, err = windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
							windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0); err != nil {
							t.Errorf("hold the other file without FILE_SHARE_DELETE: %v", err)
						}
					}
				case "placed":
					holds(t, path, "new")
				}
			}))
			if theirs != 0 {
				_ = windows.CloseHandle(theirs)
			}
			if err != nil && (!held || !strings.Contains(err.Error(), "its temp file is left")) {
				t.Errorf("a swap that left the file written a name: %v, and want the file written put in place", err)
			}
			if flushed != 1 {
				t.Errorf("the folder was flushed %d times, and want once, the temp name removed or left", flushed)
			}
			holds(t, path, "new")
			holds(t, filepath.Join(dir, "aside"), "new")
			if err != nil { // held: the write said the temp file is left, and it is the other account's
				holds(t, filepath.Join(dir, temp), "theirs")
				alone(t, dir, temp, "app.toml", "aside")
			} else {
				alone(t, dir, "app.toml", "aside")
			}
		})
	}
}

// os.SameFile tells two files apart by the volume serial and the 64-bit file index, and does not
// test the index for zero (Go 1.27.4 os/types_windows.go). A volume that gives a file no index (a
// WebDAV drive, some SMB servers) answers zero for every file, so os.SameFile would call any two the
// same and the write would not see a file swapped in at the temp name. The write reads the index of
// the handle it holds (noFileIndex zeroes it, as no test machine's volume does) and refuses with
// ErrNoFileIndex rather than trust the compare: again, reached by every write before fill and Check,
// refuses first.
//
// What it costs when this fails: on such a drive a file swapped in while the config is written is
// renamed into place over it, and the write reports done.
func TestAWriteOnAVolumeThatGivesNoFileIndexIsRefused(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"File", "FileIn"} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "app.toml")
			put0(t, path, "old")
			var err error
			switch in {
			case "File":
				err = File(path, []byte("new"), noFileIndex())
			case "FileIn":
				root, open := os.OpenRoot(dir)
				if open != nil {
					t.Fatal(open)
				}
				err = errors.Join(FileIn(root, "app.toml", []byte("new"), noFileIndex()), root.Close())
			}
			if !errors.Is(err, ErrNoFileIndex) || !errors.Is(err, errors.ErrUnsupported) {
				t.Errorf("%s on a volume that gives a file no index: %v, and want ErrNoFileIndex (matching ErrUnsupported)", in, err)
			}
			holds(t, path, "old")
			alone(t, dir, "app.toml")
		})
	}
}

// The negative of the refusal: a file in a temp dir sits on a volume that gives it a 64-bit index, so
// indexed reads it as known and a plain write is not refused. Only the seam forces the zero index.
//
// What it costs when this fails: ErrNoFileIndex fires on a normal disk, and no write can be done at
// all.
func TestARealVolumeGivesAFileAnIndexSoNoWriteIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ok, ierr := indexed(f, options{})
	if cerr := f.Close(); cerr != nil {
		t.Fatal(cerr)
	}
	if ierr != nil || !ok {
		t.Errorf("indexed on a file in a temp dir: (%v, %v), and want (true, <nil>): the volume gives a file an index", ok, ierr)
	}
	if err := File(path, []byte("new")); err != nil {
		t.Fatalf("a plain write on a real volume: %v, and want it to succeed", err)
	}
	holds(t, path, "new")
}

// The grip opens the temp file again to hold it to the rename, and checks with os.SameFile that it is
// still the file written; on a volume that gives no file index that compare would pass for any file,
// so gripTemp refuses with ErrNoFileIndex. Without the seam the index is given, so the
// grip is taken.
//
// What it costs when this fails: on a no-index drive a file swapped in at the temp name after the last
// look is gripped and renamed into place, over the one the caller wrote.
func TestTheGripRefusesAVolumeThatGivesNoFileIndex(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tmp := ".app.toml.grip.tmp" // relative to the root, as put builds it beside the name
	put0(t, filepath.Join(dir, tmp), "temp")
	mine, err := os.Lstat(filepath.Join(dir, tmp))
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	g, err := gripTemp(root, tmp, nil, mine, options{})
	if err != nil {
		t.Fatalf("gripTemp on a real volume: %v, and want the grip taken", err)
	}
	if err := g.close(); err != nil {
		t.Fatal(err)
	}

	var o options
	o.noIndex = true
	g, err = gripTemp(root, tmp, nil, mine, o)
	if g != nil || !errors.Is(err, ErrNoFileIndex) {
		t.Errorf("gripTemp on a volume that gives a file no index: (%v, %v), and want no grip and ErrNoFileIndex", g, err)
	}
}

// stays reopens the placed name to put its read-only attribute back, and checks with os.SameFile that
// the name is still the file written; on a volume that gives no file index that compare would pass for
// any file, so stays refuses with ErrNoFileIndex. Without the seam the index is given,
// so the read-only name is left as it is.
//
// What it costs when this fails: on a no-index drive a NoReplace write of a read-only file sets the
// read-only attribute on whatever file was swapped in at the name.
func TestStaysRefusesAVolumeThatGivesNoFileIndex(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	name := filepath.Join(dir, "app.toml")
	put0(t, name, "old")
	if err := os.Chmod(name, 0o400); err != nil {
		t.Fatal(err)
	}
	mine, err := os.Lstat(name)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	base := options{noReplace: true, modeSet: true}
	if err := stays(root, "app.toml", 0o400, mine, base); err != nil {
		t.Fatalf("stays on a real volume: %v, and want the read-only name left as it is", err)
	}
	o := base
	o.noIndex = true
	if err := stays(root, "app.toml", 0o400, mine, o); !errors.Is(err, ErrNoFileIndex) {
		t.Errorf("stays on a volume that gives a file no index: %v, and want ErrNoFileIndex", err)
	}
}

// again opens the temp file to read before fill writes it, and refuses a no-index volume there, so
// nothing is written and Check, which ready runs later, is never called.
//
// What it costs when this fails: on such a drive the temp file is filled and Check runs on bytes that
// may be renamed over the wrong file.
func TestAWriteRefusesBeforeItFillsOrChecksOnANoIndexVolume(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "old")
	checked := false
	err := File(path, []byte("new"), noFileIndex(), Check(func(*os.File) error { checked = true; return nil }))
	if !errors.Is(err, ErrNoFileIndex) {
		t.Errorf("a write on a no-index volume: %v, and want ErrNoFileIndex", err)
	}
	if checked {
		t.Error("Check was called though the volume gives a file no index and nothing is to be written")
	}
	holds(t, path, "old")
	alone(t, dir, "app.toml")
}

// Keep copies the file by reading it through regular, which refuses a no-index volume, so no copy is
// made.
//
// What it costs when this fails: on such a drive Keep copies a file swapped in for the one it looked
// at, and a later PutBack restores the wrong bytes.
func TestKeepRefusesANoIndexVolume(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "before")
	if _, err := Keep(path, dir, "app.toml.kept", noFileIndex()); !errors.Is(err, ErrNoFileIndex) {
		t.Errorf("Keep on a no-index volume: %v, and want ErrNoFileIndex", err)
	}
	alone(t, dir, "app.toml")
}

// PutBack reads the kept copy through regular, which refuses a no-index volume before it makes a
// temp file: the "temp" step is never reached, so the refusal is regular's and not again's. The copy
// is in another folder than the file.
//
// What it costs when this fails: on such a drive PutBack restores a file swapped in for the kept copy,
// over the live file.
func TestPutBackRefusesANoIndexVolumeForAKeptCopyInAnotherFolder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sub := filepath.Join(dir, "kept")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "app.toml")
	put0(t, path, "before")
	kept, err := Keep(path, sub, "app.toml.kept")
	if err != nil {
		t.Fatal(err)
	}
	if err := File(path, []byte("after")); err != nil {
		t.Fatal(err)
	}
	reachedTemp := false
	step := stepWith(func(at string) {
		if at == "temp" {
			reachedTemp = true
		}
	})
	if err := PutBack(kept, path, sha256.Sum256([]byte("after")), noFileIndex(), step); !errors.Is(err, ErrNoFileIndex) {
		t.Errorf("PutBack on a no-index volume: %v, and want ErrNoFileIndex", err)
	}
	if reachedTemp {
		t.Error("PutBack reached the \"temp\" step; it should refuse in regular, reading the kept copy, before it makes a temp file")
	}
	holds(t, path, "after")
}

// Append holds the log open and checks with os.SameFile that the name is still it, which a no-index
// volume would pass for any file; told on the held handle refuses first, so no line is written and
// the "moved by another writer" path (return true, nil) is not taken.
//
// What it costs when this fails: on such a drive a log moved aside and replaced is taken as the held
// log, and the line is appended to the moved one.
func TestAppendRefusesANoIndexVolume(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	put0(t, filepath.Join(dir, "audit.log"), "line1\n")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	err = appendWith(t.Context(), root, "audit.log", []byte("line2"), 100, settle([]Option{asideWith("audit.log.1"), noFileIndex()}))
	if !errors.Is(err, ErrNoFileIndex) {
		t.Errorf("Append on a no-index volume: %v, and want ErrNoFileIndex", err)
	}
	holds(t, filepath.Join(dir, "audit.log"), "line1\n")
}

// Swap keeps no open handle on the files it moves, so it opens old and nu to read and refuses a
// no-index volume before it moves anything; the file and the new file stay where they are.
//
// What it costs when this fails: on such a drive a file swapped in at the aside or the program name is
// taken as the one Swap moved, and the running program is replaced with the wrong file.
func TestSwapRefusesANoIndexVolume(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file, fresh, aside := filepath.Join(dir, "app"), filepath.Join(dir, "app.new"), filepath.Join(dir, "app.old")
	put0(t, file, "old")
	put0(t, fresh, "new")
	if err := Swap(file, fresh, aside, noFileIndex()); !errors.Is(err, ErrNoFileIndex) {
		t.Errorf("Swap on a no-index volume: %v, and want ErrNoFileIndex", err)
	}
	holds(t, file, "old")
	holds(t, fresh, "new")
	alone(t, dir, "app", "app.new")
}

// toldAt reads the index of each name Swap will move and holds it to the look Swap took, by
// os.SameFile: a file swapped in at file's name between the look and the index read is caught, and
// Swap moves nothing (the "swap" step, after the reads, is never reached).
//
// What it costs when this fails: a file swapped in at the program's name after Swap's look is moved
// aside as the old program and taken for it, where the index read was of a file tied to nothing.
func TestSwapRefusesAFileSwappedInBeforeTheIndexRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file, fresh, aside := filepath.Join(dir, "app"), filepath.Join(dir, "app.new"), filepath.Join(dir, "app.old")
	put0(t, file, "old")
	put0(t, fresh, "new")
	put0(t, filepath.Join(dir, "theirs"), "theirs")
	swappedIn, reachedSwap := false, false
	step := stepWith(func(at string) {
		switch at {
		case "told":
			if swappedIn {
				return
			}
			swappedIn = true
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Error(err)
				return
			}
			if err := root.Rename("theirs", "app"); err != nil { // POSIX rename over the name, by the root
				t.Errorf("rename another file over file's name: %v", err)
			}
			_ = root.Close()
		case "swap":
			reachedSwap = true
		}
	})
	err := Swap(file, fresh, aside, step)
	if !swappedIn {
		t.Fatal("the step never reached \"told\", so no file was swapped in")
	}
	if err == nil || !strings.Contains(err.Error(), "changed between its look and the read of its file index") {
		t.Errorf("Swap with a file swapped in at file before the index read: %v, and want it refused as changed", err)
	}
	if reachedSwap {
		t.Error("Swap reached the \"swap\" step; it should refuse in toldAt, before moving anything")
	}
}

// A NoReplace write whose hard link the volume answers with ERROR_INVALID_FUNCTION or
// ERROR_NOT_SUPPORTED, as a volume that keeps access lists and no hard links does (some SMB
// servers; FAT and exFAT are refused before, with ErrNoAccessList), is refused with ErrNoHardLinks and the
// volume's own answer, and leaves nothing at the name and no temp file. A link denied is its own
// error, never ErrNoHardLinks.
//
// What it costs when this fails: a program on such a volume is told the name was taken, or that
// it was denied, and tries again or asks for rights, where no NoReplace write there can ever
// succeed.
func TestANoReplaceWriteOnAVolumeWithNoHardLinksSaysErrNoHardLinks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	for _, refusal := range []error{windows.ERROR_INVALID_FUNCTION, windows.ERROR_NOT_SUPPORTED} {
		asked := 0
		answer := linkWith(func() error { asked++; return refusal })
		err := File(path, []byte("new"), NoReplace(), answer)
		if !errors.Is(err, ErrNoHardLinks) || !errors.Is(err, refusal) {
			t.Errorf("a NoReplace write whose link is answered with %v: %v, and want ErrNoHardLinks with it", refusal, err)
		}
		if asked != 1 {
			t.Errorf("the link was answered %d times by a write refused with %v, and want once", asked, refusal)
		}
		alone(t, dir)
	}
	denied := linkWith(func() error { return windows.ERROR_ACCESS_DENIED })
	if err := File(path, []byte("new"), NoReplace(), denied); errors.Is(err, ErrNoHardLinks) || !errors.Is(err, fs.ErrPermission) {
		t.Errorf("a NoReplace write whose link is denied: %v, and want fs.ErrPermission, not ErrNoHardLinks", err)
	}
	alone(t, dir)
}

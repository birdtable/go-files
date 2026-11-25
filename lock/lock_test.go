package lock

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func tempLock(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "app.lock")
}

func take(t *testing.T, path string) *Lock {
	t.Helper()
	l, err := Take(path, path+".holder")
	if err != nil {
		t.Fatalf("Take(%s): %v", path, err)
	}
	t.Cleanup(func() { _ = l.Release() })
	return l
}

// Take writes no note: the lock's folder holds no <name>.holder, and a second Take is declined
// with Held{}, "another process". Once the holder calls Note, a second Take is told who.
//
// What it costs when this fails: two runs change the machine at once; or a program whose lock is
// in a folder other accounts read publishes its process there without asking; or
// a refused run is told nothing when its holder said who it is.
func TestOneRunGetsTheLockAndTheOtherIsToldWhoOnlyOnceItNotes(t *testing.T) {
	t.Parallel()
	path := tempLock(t)
	first := take(t, path)
	if _, err := os.Lstat(path + ".holder"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Take wrote a note: %v, and want none", err)
	}
	l, err := Take(path, path+".holder")
	var h Held
	if l != nil || !errors.As(err, &h) || h != (Held{}) || err.Error() != "another process holds the lock" {
		t.Fatalf("a second Take with no note: %v, %#v, and want Held{}", l, err)
	}
	if err := first.Note(nil); err != nil {
		t.Fatal(err)
	}
	l, err = Take(path, path+".holder")
	if l != nil || !errors.As(err, &h) || h.By != os.Getpid() || time.Since(h.Since) > time.Hour {
		t.Fatalf("a second Take: %v, %#v, and want Held naming this process", l, err)
	}
	if !strings.Contains(err.Error(), "process "+strconv.Itoa(os.Getpid())+" has held the lock since") {
		t.Errorf("the refusal does not say who: %v", err)
	}
}

// Two processes: the second Take is declined and Holder names the first and its note; Wait wins
// once the first releases.
//
// What it costs when this fails: two programs change the machine at once, or a refused one cannot
// say which process to wait for.
func TestASecondProcessIsDeclinedAndToldWhoAndWaitWinsAfterTheRelease(t *testing.T) {
	t.Parallel()
	path := tempLock(t)
	cmd, in := holding(t, path)
	pid := cmd.Process.Pid
	var h Held
	if _, err := Take(path, path+".holder"); !errors.As(err, &h) || h.By != pid {
		t.Fatalf("a Take beside the helper %d: %v", pid, err)
	}
	h, note, err := Holder(path + ".holder")
	if err != nil || h.By != pid || string(note) != `{"by":"helper"}` {
		t.Fatalf("Holder: %#v, %s, %v, and want the helper %d and its note", h, note, err, pid)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	l, err := Wait(t.Context(), path, path+".holder")
	if err != nil {
		t.Fatalf("Wait after the helper released: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Error(err)
	}
}

// A holder killed (SIGKILL, TerminateProcess as taskkill /F) never releases, and the next lock
// wins all the same, and removes the note the killed holder left. It is a Wait with no deadline:
// Windows drops a dead process's lock when it has the resources to, a moment after the process
// ends, which no test can time.
//
// What it costs when this fails: a crashed run shuts every later run out until a person removes a
// file.
func TestAKilledHoldersLockIsTakenByTheNext(t *testing.T) {
	t.Parallel()
	path := tempLock(t)
	cmd, _ := holding(t, path)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait() // it was killed, so it fails
	if h, _, err := Holder(path + ".holder"); err != nil || h.By != cmd.Process.Pid {
		t.Fatalf("the killed holder's note: %#v, %v, and want it left naming %d", h, err, cmd.Process.Pid)
	}
	l, err := Wait(t.Context(), path, path+".holder")
	if err != nil {
		t.Fatalf("the lock of a killed holder: %v", err)
	}
	if _, err := os.Lstat(path + ".holder"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the killed holder's note outlived the next Wait: %v, and want it removed, as it names a process that holds nothing", err)
	}
	if err := l.Release(); err != nil {
		t.Error(err)
	}
}

// Wait, in a bubble whose clock is fake, waits for the holder to its deadline and then fails
// with the context's error and Held; a Wait whose holder lets go mid-wait takes the lock.
//
// What it costs when this fails: a run told to wait gives up at once, waits past its deadline, or
// cannot tell a deadline from a broken lock.
func TestWaitWaitsToItsDeadlineAndTakesALockLetGo(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		path := tempLock(t)
		first := take(t, path)
		start := time.Now()
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		_, err := Wait(ctx, path, path+".holder")
		if !errors.Is(err, context.DeadlineExceeded) || !IsHeld(err) {
			t.Errorf("a Wait to its deadline: %v, and want DeadlineExceeded and Held", err)
		}
		if waited := time.Since(start); waited < time.Second {
			t.Errorf("Wait gave up after %v of its second", waited)
		}

		got := make(chan error, 1)
		go func() {
			l, err := Wait(t.Context(), path, path+".holder")
			if err == nil {
				err = l.Release()
			}
			got <- err
		}()
		synctest.Wait()
		select {
		case err := <-got:
			t.Fatalf("Wait ended while the lock was held: %v", err)
		default:
		}
		if err := first.Release(); err != nil {
			t.Fatal(err)
		}
		if err := <-got; err != nil {
			t.Errorf("Wait after the release: %v", err)
		}
	})
}

// What it costs when this fails: a run that has finished still shuts out the next one, or a
// deferred second release turns a clean exit into an error.
func TestReleasingLetsTheNextRunIn(t *testing.T) {
	t.Parallel()
	path := tempLock(t)
	first, err := Take(path, path+".holder")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Note(nil); err != nil {
		t.Fatal(err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	if err := first.Release(); err != nil {
		t.Errorf("a second Release: %v", err)
	}
	if _, err := os.Lstat(path + ".holder"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the note outlived the release: %v", err)
	}
	take(t, path)
	var l *Lock
	if err := l.Release(); err != nil {
		t.Errorf("a nil Lock's Release: %v", err)
	}
	if IsHeld(errors.New("disk full")) {
		t.Error("an error that is not Held reads as held")
	}
}

// A lock file with bytes in it, an old lock's note among them, is taken and left byte for byte. The
// bytes are read once the lock is released: on Windows the lock is a byte-range lock the system
// enforces, so a read while it is held is refused.
//
// What it costs when this fails: a lock file left by an older version shuts the program out, or
// a file Take is pointed at is written over.
func TestALockFileWithBytesInItIsTakenAndLeftAsItWas(t *testing.T) {
	t.Parallel()
	path := tempLock(t)
	old := []byte("{\"pid\":1,\"binary\":\"old\"}\nmore\n")
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	l := take(t, path)
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, old) {
		t.Errorf("the lock file holds %q, %v, and want %q", got, err, old)
	}
}

// A .holder cut at every byte reads as held by another process, never as free and never as an
// error; whole, it names the holder and its note.
//
// What it costs when this fails: a holder killed mid-write, or a note cut short, reads as a lock
// nobody holds, or fails the run that asked who holds it.
func TestATornNoteReadsAsHeldAtEveryByte(t *testing.T) {
	t.Parallel()
	path := tempLock(t)
	l := take(t, path)
	if err := l.Note([]string{"build", "7"}); err != nil {
		t.Fatal(err)
	}
	whole, err := os.ReadFile(path + ".holder")
	if err != nil {
		t.Fatal(err)
	}
	cut := 0
	for n := range len(whole) {
		if err := os.WriteFile(path+".holder", whole[:n], 0o600); err != nil {
			t.Fatal(err)
		}
		h, note, err := Holder(path + ".holder")
		if err != nil || h != (Held{}) || note != nil {
			t.Fatalf("cut at %d of %d: %#v, %q, %v, and want Held{} and no note", n, len(whole), h, note, err)
		}
		cut++
	}
	if cut != len(whole) || cut < 20 {
		t.Fatalf("cut %d of %d bytes", cut, len(whole))
	}
	if err := os.WriteFile(path+".holder", whole, 0o600); err != nil {
		t.Fatal(err)
	}
	if h, note, err := Holder(path + ".holder"); err != nil || h.By != os.Getpid() || string(note) != `["build","7"]` {
		t.Errorf("whole: %#v, %s, %v", h, note, err)
	}
	if h, note, err := Holder(tempLock(t) + ".holder"); err != nil || h != (Held{}) || note != nil {
		t.Errorf("no note at all: %#v, %q, %v", h, note, err)
	}
	over := `{"pid":5}` + strings.Repeat(" ", maxNote-len(`{"pid":5}`)) + "\n"
	for name, body := range map[string]string{
		"pid 0":                 `{"pid":0,"note":"x"}` + "\n",
		"pid -1":                `{"pid":-1,"note":"x"}` + "\n",
		"one byte over the cap": over,
	} {
		if err := os.WriteFile(path+".holder", []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if h, note, err := Holder(path + ".holder"); err != nil || h != (Held{}) || note != nil {
			t.Errorf("%s: %#v, %q, %v, and want Held{} and no note", name, h, note, err)
		}
	}
	if len(over) != maxNote+1 {
		t.Fatalf("the note over the cap is %d bytes", len(over))
	}
}

// lock locks the file it checked by flock(2) (flock.go) on the Unix systems that have it and by
// LockFileEx (lock_windows.go) on Windows, imports gofrs/flock on none, and on aix and solaris,
// whose one lock is fcntl's, does not build. This is read from
// the files without the go tool, which a test cannot reach without a network.
//
// What it costs when this fails: a system locks a file a second way, or lock builds on a system
// whose lock a second open in the same process wins.
func TestEverySystemLocksItsCheckedFileAndFcntlSystemsDoNotBuild(t *testing.T) {
	t.Parallel()
	read := 0
	for _, goos := range []string{"linux", "darwin", "windows", "freebsd", "netbsd", "aix", "solaris"} {
		ctx := build.Default
		ctx.GOOS, ctx.CgoEnabled = goos, false
		pkg, err := ctx.ImportDir(".", 0)
		if err != nil {
			t.Fatal(err)
		}
		fcntl := goos == "aix" || goos == "solaris"
		locks := slices.Contains(pkg.GoFiles, "flock.go") || slices.Contains(pkg.GoFiles, "lock_windows.go")
		if locks == fcntl || slices.Contains(pkg.Imports, "github.com/gofrs/flock") {
			t.Errorf("for %s the files are %v and the imports %v", goos, pkg.GoFiles, pkg.Imports)
		}
		read++
	}
	if read != 7 {
		t.Fatalf("read %d systems", read)
	}
}

// File is refused with ErrNoInherit, which matches errors.ErrUnsupported, on every system: Release
// drops the lock for every copy of the lock file, so a child given it would not keep the lock. The
// lock is still held, and Release drops it.
//
// What it costs when this fails: a caller hands a child the lock file to keep a job slot, and the
// slot is free while the child runs, so two jobs share it.
func TestFileIsRefusedWithErrNoInherit(t *testing.T) {
	t.Parallel()
	path := tempLock(t)
	l := take(t, path)
	if f, err := l.File(); f != nil || !errors.Is(err, ErrNoInherit) || !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("File: %v, %v, and want ErrNoInherit matching errors.ErrUnsupported", f, err)
	}
	if _, err := Take(path, path+".holder"); !IsHeld(err) {
		t.Fatalf("after File was refused: %v, and want Held", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	take(t, path)
}

// Stat is the lock file's, the one at the lock's name when Take took it, and after Release, or
// on a nil Lock, it is fs.ErrClosed.
//
// What it costs when this fails: a caller that checks the lock it holds is the file at the name
// it reached (a state folder, by os.Root) cannot, now that File is refused, or checks the
// wrong file and takes a moved folder's lock as its own.
func TestStatIsTheLockFilesAndClosedAfterRelease(t *testing.T) {
	t.Parallel()
	path := tempLock(t)
	l := take(t, path)
	at, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := l.Stat(); err != nil || !os.SameFile(got, at) {
		t.Fatalf("Stat: %v, %v, and want the lock file", got, err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	var none *Lock
	for name, s := range map[string]*Lock{"after Release": l, "on nil": none} {
		if got, err := s.Stat(); got != nil || !errors.Is(err, fs.ErrClosed) {
			t.Errorf("Stat %s: %v, %v, and want fs.ErrClosed", name, got, err)
		}
	}
}

// The names the lock dropped are not exported again: `go doc -all` at a tag is the record, this
// the check. TakeIn and WaitIn, dropped at v0.1.0 with their options, came back with a note and no
// option, so they are not on the list.
//
// What it costs when this fails: a caller builds on Mode, Group or Option again, and the next cut
// breaks it.
func TestTheDroppedNamesAreNotExported(t *testing.T) {
	t.Parallel()
	pkgs, err := parser.ParseDir(token.NewFileSet(), ".", func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0) //nolint:staticcheck // ParseDir reads one folder's files, which is all this needs
	if err != nil {
		t.Fatal(err)
	}
	dropped := []string{"Mode", "Group", "Option", "ErrNotPermitted", "ErrNotPlain", "ErrNotTried", "ErrOutside"}
	seen := 0
	for _, p := range pkgs {
		for _, f := range p.Files {
			for name, obj := range f.Scope.Objects {
				if slices.Contains(dropped, name) {
					t.Errorf("%s is declared again (%v)", name, obj.Kind)
				}
				if ast.IsExported(name) {
					seen++
				}
			}
		}
	}
	if seen < 4 {
		t.Fatalf("saw %d exported names", seen)
	}
}

// Note refuses a note whose line is over the cap Holder reads, before it writes anything, and the
// note before it stays and is read back; a line of the cap exactly is written and read back.
//
// What it costs when this fails: a holder is told its note is in place when no reader will ever
// see it, and the process and time that were readable before are gone too.
func TestANoteOverTheCapIsRefusedAndTheOneBeforeItStays(t *testing.T) {
	t.Parallel()
	path := tempLock(t)
	l := take(t, path)
	if err := l.Note(""); err != nil {
		t.Fatal(err)
	}
	empty, err := os.ReadFile(path + ".holder")
	if err != nil {
		t.Fatal(err)
	}
	fits := strings.Repeat("x", maxNote-len(empty))
	if err := l.Note(fits); err != nil {
		t.Fatalf("a note of %d bytes, the cap: %v", maxNote, err)
	}
	if fi, err := os.Stat(path + ".holder"); err != nil || fi.Size() != maxNote {
		t.Fatalf("the note at the cap: %v, %v", fi, err)
	}
	if h, note, err := Holder(path + ".holder"); err != nil || h.By != os.Getpid() || string(note) != strconv.Quote(fits) {
		t.Fatalf("Holder of a note at the cap: %#v, %d bytes, %v", h, len(note), err)
	}
	for _, over := range []string{fits + "x", strings.Repeat("x", 70000)} {
		if err := l.Note(over); err == nil {
			t.Errorf("a note of %d bytes was taken", len(empty)+len(over))
		}
		if h, note, err := Holder(path + ".holder"); err != nil || h.By != os.Getpid() || string(note) != strconv.Quote(fits) {
			t.Errorf("after a note of %d bytes, Holder: %#v, %d bytes, %v, and want the note before it", len(over), h, len(note), err)
		}
	}
	left, err := filepath.Glob(path + ".holder.*")
	if err != nil || len(left) != 0 {
		t.Errorf("temp files left: %v, %v", left, err)
	}
}

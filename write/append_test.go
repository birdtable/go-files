package write

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test waits on two appenders it started as helper processes
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// appendIn appends line to name in dir with limit, through a root of dir.
func appendIn(t *testing.T, dir, name, line string, limit int64) error {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	return Append(t.Context(), root, name, []byte(line), limit, Aside(name+".1"))
}

// The cap holds at its boundary: a line that brings the log to exactly the cap stays in it, one
// byte more moves the log to .1 first, and a line longer than the cap is a log of its own, whole.
//
// What it costs when this fails: a log grows past its cap and fills the disk, or a line is cut
// in two across the move and a reader parses half of it.
func TestAppendKeepsTheCapAndNeverCutsALine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := filepath.Join(dir, "audit.log")
	steps := []struct{ line, log, old string }{
		{"12345678", "12345678\n", ""},
		{"", "12345678\n\n", ""},     // 9 + 1 = 10, the cap exactly
		{"x", "x\n", "12345678\n\n"}, // 10 + 2 > 10: moved first
		{"a line longer than the cap", "a line longer than the cap\n", "x\n"}, // a log of its own
		{"y", "y\n", "a line longer than the cap\n"},
	}
	done := 0
	for _, s := range steps {
		if err := appendIn(t, dir, "audit.log", s.line, 10); err != nil {
			t.Fatalf("after %q: %v", s.line, err)
		}
		holds(t, log, s.log)
		if s.old != "" {
			holds(t, log+".1", s.old)
		}
		done++
	}
	if done != len(steps) {
		t.Fatalf("took %d of %d steps", done, len(steps))
	}
	permIs(t, log, 0o600)
	alone(t, dir, "audit.log", "audit.log.1")
}

// Aside names where a full log moves, over a plain file there, and nothing is put at <name>.1; a
// link at Aside's name is refused as one at <name>.1 is.
//
// What it costs when this fails: a caller's rotation reads its old log where Append never puts
// it, or a link at the caller's name moves the log over a file of an attacker's choosing.
func TestAsideNamesWhereAFullLogMoves(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	put0(t, filepath.Join(dir, "audit.prev"), "older")
	for _, line := range []string{"12345678", "x"} {
		if err := Append(t.Context(), root, "audit.log", []byte(line), 10, Aside("audit.prev")); err != nil {
			t.Fatalf("after %q: %v", line, err)
		}
	}
	holds(t, filepath.Join(dir, "audit.log"), "x\n")
	holds(t, filepath.Join(dir, "audit.prev"), "12345678\n")
	alone(t, dir, "audit.log", "audit.prev")

	target := filepath.Join(dir, "target")
	put0(t, target, "theirs")
	plantLink(t, "target", filepath.Join(dir, "b.prev"))
	put0(t, filepath.Join(dir, "b.log"), "full")
	if err := Append(t.Context(), root, "b.log", []byte("line"), 5, Aside("b.prev")); err == nil {
		t.Error("Append moved the log over a link at Aside's name")
	}
	holds(t, target, "theirs")
	holds(t, filepath.Join(dir, "b.log"), "full")
}

// What Append's options cannot do is refused before any file is opened or made, and no line is
// written: no name aside, as Append takes no default (ErrNoAside), a log moved aside
// to its own name as written or cleaned, and a LogMode by which the log's owner cannot read and
// write it (Windows refuses every LogMode, by ErrNoMode), and Shared, File's alone, reached by a
// conversion to LogOption. Its own name
// in another case, where the volume ignores case, is refused at the move, and no line is written.
//
// What it costs when this fails: a full log goes to a name the program never chose, a log moved
// over itself loses every line it held, or a log made 0200 takes one line and refuses every one after it, as Append opens it to read too.
func TestAppendRefusesOptionsItCannotKeepAndWritesNoLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	cases := []struct {
		what string
		opts []LogOption
		want error // nil for any error
	}{
		{"no Aside", nil, ErrNoAside},
		{"Aside the empty name", []LogOption{Aside("")}, ErrNoAside},
		{"no Aside, with a LogMode it keeps", []LogOption{LogMode(0o600)}, ErrNoAside},
		{"Aside the log's own name", []LogOption{Aside("c.log")}, nil},
		{"Aside the log's own name, spelled ./", []LogOption{Aside("./c.log")}, nil},
		{"Aside the log's own name, spelled with ..", []LogOption{Aside("sub/../c.log")}, nil},
		{"LogMode 0400", []LogOption{Aside("c.log.1"), LogMode(0o400)}, nil},
		{"LogMode 0200", []LogOption{Aside("c.log.1"), LogMode(0o200)}, nil},
		{"LogMode 0044", []LogOption{Aside("c.log.1"), LogMode(0o044)}, nil},
		{"Shared, File's", []LogOption{Aside("c.log.1"), LogOption(Shared())}, fs.ErrInvalid},
	}
	ran := 0
	for _, c := range cases {
		err := Append(t.Context(), root, "c.log", []byte("line"), 100, c.opts...)
		if err == nil || c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("%s: Append answered %v, and want a refusal (%v where set)", c.what, err, c.want)
		}
		alone(t, dir)
		ran++
	}
	if ran != len(cases) {
		t.Fatalf("ran %d of %d cases", ran, len(cases))
	}
	put0(t, filepath.Join(dir, "c.log"), "full")
	if err := Append(t.Context(), root, "c.log", []byte("line"), 5, Aside("c.log")); err == nil {
		t.Error("a full log was moved aside to its own name")
	}
	holds(t, filepath.Join(dir, "c.log"), "full")
	alone(t, dir, "c.log")
	// Another case is the log's own name where the volume ignores case, as macOS's and Windows'
	// usually do, and is refused at the move; where the volume keeps case it is another file,
	// which takes the log. The temp folder's volume says which.
	upper, uerr := root.Lstat("C.LOG") // not there where the volume keeps case
	low, lerr := root.Lstat("c.log")
	if lerr != nil {
		t.Fatal(lerr)
	}
	err = Append(t.Context(), root, "c.log", []byte("line"), 5, Aside("C.LOG"))
	if uerr == nil && os.SameFile(upper, low) {
		if err == nil {
			t.Error("a full log was moved aside to its own name in another case, on a volume that ignores case")
		}
		holds(t, filepath.Join(dir, "c.log"), "full")
		alone(t, dir, "c.log")
		return
	}
	if err != nil {
		t.Errorf("a full log moved aside to C.LOG, on a volume that keeps case: %v", err)
	}
	holds(t, filepath.Join(dir, "C.LOG"), "full")
	holds(t, filepath.Join(dir, "c.log"), "line\n")
}

// Two processes appending 1000 lines each at once, across one move of the log, lose no line, cut
// none, and keep each writer's lines in its order.
//
// What it costs when this fails: an audit log loses the line that recorded what a second
// process did, or holds two lines spliced into one.
func TestAppendFromTwoProcessesLosesAndCutsNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := filepath.Join(dir, "audit.log")
	const n, limit = 1000, 8000 // 11780 bytes in all: the log moves once
	var cmds []*exec.Cmd
	for _, tag := range []string{"a", "b"} {
		cmd := helperCmd(t, "append", log, tag, strconv.Itoa(n), strconv.Itoa(limit))
		cmd.Stderr = &strings.Builder{}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, cmd)
	}
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("a writer: %v %s", err, cmd.Stderr)
		}
	}
	countLines(t, log, n, limit)
}

// countLines fails t unless log.1 and then log hold n lines of each writer, each whole and in its
// order, neither file over limit.
func countLines(t *testing.T, log string, n int, limit int64) {
	t.Helper()
	next := map[string]int{}
	for _, path := range []string{log + ".1", log} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(b)) > limit {
			t.Errorf("%s is %d bytes, over the cap of %d", path, len(b), limit)
		}
		for _, line := range strings.SplitAfter(string(b), "\n") {
			if line == "" {
				continue
			}
			tag, i, ok := strings.Cut(strings.TrimSuffix(line, "\n"), " ")
			if !ok || !strings.HasSuffix(line, "\n") || i != strconv.Itoa(next[tag]) {
				t.Fatalf("%s holds %q where writer %s's line %d is due", path, line, tag, next[tag])
			}
			next[tag]++
		}
	}
	if next["a"] != n || next["b"] != n || len(next) != 2 {
		t.Errorf("the logs hold %v lines, and want %d of a and of b", next, n)
	}
}

// A link at the log's name, or at .1 when the log is to move over it, is refused, and the line
// goes nowhere; what the link leads to is not touched.
//
// What it costs when this fails: a link planted at a log's name makes a privileged writer
// append to, or move a log over, a file of the attacker's choosing.
func TestAppendRefusesALinkAtTheNameAndAtTheOldLog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	put0(t, target, "theirs")
	plantLink(t, "target", filepath.Join(dir, "a.log"))
	if err := appendIn(t, dir, "a.log", "line", 100); err == nil {
		t.Error("Append wrote through a link at the name")
	}
	holds(t, target, "theirs")

	put0(t, filepath.Join(dir, "b.log"), "full")
	plantLink(t, "target", filepath.Join(dir, "b.log.1"))
	if err := appendIn(t, dir, "b.log", "line", 5); err == nil {
		t.Error("Append moved the log over a link at .1")
	}
	holds(t, target, "theirs")
	holds(t, filepath.Join(dir, "b.log"), "full")
	if fi, err := os.Lstat(filepath.Join(dir, "b.log.1")); err != nil || fi.Mode().IsRegular() {
		t.Errorf("b.log.1 is %v (%v), and want the link left", fi, err)
	}

	if !panics(func() { _ = Append(t.Context(), nil, "c.log", []byte("x"), 10, Aside("c.log.1")) }) {
		t.Error("Append with no root did not panic, as a nil argument does")
	}
}

// A log another writer moved between this writer's open and its hold is left as it is, and the
// line goes into the log at the name.
//
// What it costs when this fails: a line written as the log is moved lands in the old log, past
// its cap, and the next move throws it away.
func TestALogMovedBeforeItIsHeldGetsNoLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := filepath.Join(dir, "audit.log")
	put0(t, log, "old\n")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	moved := false
	err = appendWith(t.Context(), root, "audit.log", []byte("line"), 100, settle([]Option{asideWith("audit.log.1"), stepWith(func(at string) {
		if at == "held" && !moved {
			moved = true
			if err := os.Rename(log, log+".moved"); err != nil {
				t.Fatal(err)
			}
			put0(t, log, "")
		}
	})}))
	if err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Fatal("the log was never held")
	}
	holds(t, log, "line\n")
	holds(t, log+".moved", "old\n")
}

// A log with a second name, a hard link to a file outside the root, is refused, and the line goes
// nowhere: what the link leads to is not touched.
//
// What it costs when this fails: whoever can make a name in the log's folder, on a system that
// lets them link a file they cannot write, has a privileged writer append to a file outside the
// root, which os.Root cannot see, since no name leads out.
func TestALogWithASecondNameIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in"), filepath.Join(dir, "out")
	for _, d := range []string{in, out} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(out, "target")
	put0(t, target, "theirs\n")
	if err := os.Link(target, filepath.Join(in, "audit.log")); err != nil {
		t.Fatal(err)
	}
	if err := appendIn(t, in, "audit.log", "line", 100); err == nil {
		t.Error("Append wrote through a hard link to a file outside the root")
	}
	holds(t, target, "theirs\n")
}

// Another writer's move of the log between this writer's look and its open, the log gone at the
// open or a new log there, sends this writer back to look again, and the line goes into the log at
// the name.
//
// What it costs when this fails: with two writers at the cap, a line now and then is refused for
// the other writer's move, and an audit line is not written.
func TestAMoveBetweenTheLookAndTheOpenIsLookedAtAgain(t *testing.T) {
	t.Parallel()
	moves := map[string]func(log string) error{
		"gone":    func(log string) error { return os.Rename(log, log+".1") },
		"new log": func(log string) error { return errors.Join(os.Rename(log, log+".1"), os.WriteFile(log, nil, 0o600)) },
	}
	ran := 0
	for kind, move := range moves {
		dir := t.TempDir()
		log := filepath.Join(dir, "audit.log")
		put0(t, log, "old\n")
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		moved := false
		err = appendWith(t.Context(), root, "audit.log", []byte("line"), 100, settle([]Option{asideWith("audit.log.1"), stepWith(func(at string) {
			if at == "look" && !moved {
				moved = true
				if err := move(log); err != nil {
					t.Fatal(err)
				}
			}
		})}))
		_ = root.Close()
		if !moved {
			t.Fatalf("%s: the step look was never reached", kind)
		}
		if err != nil {
			t.Errorf("%s: %v, and want the line in the log at the name", kind, err)
		}
		holds(t, log, "line\n")
		holds(t, log+".1", "old\n")
		ran++
	}
	if ran != len(moves) {
		t.Fatalf("ran %d of %d moves", ran, len(moves))
	}
}

// A hold another process keeps on the log, a reader's shared one included, is waited on until the
// caller's context ends, and then refused at once with the context's error: one already canceled,
// after its one try, and one whose deadline passes, as it passes. The line goes nowhere, and once
// the hold is let go a line is written.
//
// What it costs when this fails: any reader of a log that can open it takes a hold and stops
// every line the program writes, and the program with it, past the caller's own deadline.
func TestALogHeldByAnotherProcessIsRefusedOnceTheContextEnds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := filepath.Join(dir, "audit.log")
	put0(t, log, "old\n")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	letGoOf := holdShared(t, log)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Append(canceled, root, "audit.log", []byte("line"), 100, Aside("audit.log.1")); !errors.Is(err, context.Canceled) {
		t.Errorf("Append on a held log with a canceled context: %v, and want context.Canceled", err)
	}
	holds(t, log, "old\n")
	deadline, stop := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer stop()
	start := time.Now()
	err = Append(deadline, root, "audit.log", []byte("line"), 100, Aside("audit.log.1"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Append on a held log past the context's deadline: %v, and want context.DeadlineExceeded", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("Append on a held log with a deadline 50ms on returned after %v, not at once", took)
	}
	holds(t, log, "old\n")
	letGoOf()
	if err := Append(t.Context(), root, "audit.log", []byte("line"), 100, Aside("audit.log.1")); err != nil {
		t.Fatalf("Append once the hold is let go: %v", err)
	}
	holds(t, log, "old\nline\n")
}

// With a context that has no deadline, Append waits for another process's hold on the log for as
// long as it is kept, with no limit of its own, and writes the line once the hold is let go.
//
// What it costs when this fails: a caller that chose to wait for its log loses a line to a limit
// it did not set, and cannot tell a slow reader from a stuck one.
func TestALogHeldWithNoDeadlineIsWaitedOnUntilItsHolderLetsGo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := filepath.Join(dir, "audit.log")
	put0(t, log, "old\n")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	letGoOf := holdShared(t, log)
	done := make(chan error, 1)
	go func() {
		done <- Append(context.Background(), root, "audit.log", []byte("line"), 100, Aside("audit.log.1"))
	}()
	select {
	case err := <-done:
		t.Fatalf("Append on a held log with no deadline returned while the log was held: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	holds(t, log, "old\n")
	letGoOf()
	if err := <-done; err != nil {
		t.Fatalf("Append once the hold is let go: %v", err)
	}
	holds(t, log, "old\nline\n")
}

// A link put at the log after the look, which the open follows to a file of one name, and swapped
// for a hard link to that file before the hold's check, is refused by the count of names taken
// once the log is held, and the file gets no line. The third swap takes the hard link's name away
// again after the check and before the count: by a remove; by a rename over it, the standard
// library's and the root's; by a rename over it of another hard link to the held file, then a
// remove; and by a hard link made over it with replace, plain and with POSIX semantics, which
// os.Link and os.Root.Link never ask. On Unix the count is read from the look the check matched,
// and each act is done; on Windows the held log is pinned by its name, so the count sees two names
// and no line lands in the other file inside the root. The
// first three acts are refused there. Of the two renames of a hard link only Append's refusal is
// judged, as NTFS may refuse a part of the act or carry the pin to the name that stays, and either
// holds; a hard link made over the name is done or refused by the pin, and any other failure is
// linkOver's own and fails the row.
//
// What it costs when this fails: swaps timed to the look and the hold have a privileged writer
// append to a file of the swapper's choosing.
func TestALinkSwappedForAHardLinkBeforeTheHoldGetsNoLine(t *testing.T) {
	t.Parallel()
	const name = "audit-trail.log"
	type swap struct {
		row     string
		checked func(t *testing.T, root *os.Root, dir, log string) error // nil for two swaps
		judge   func(t *testing.T, act string, err error) error          // the act's own result on Windows; nil for held's
	}
	again := func(rename func(root *os.Root, dir, log string) error) func(*testing.T, *os.Root, string, string) error {
		return func(_ *testing.T, root *os.Root, dir, log string) error {
			return errors.Join(os.Link(filepath.Join(dir, "target"), filepath.Join(dir, "again")), rename(root, dir, log), os.Remove(log))
		}
	}
	rows := []swap{
		{"two swaps", nil, nil},
		{"the third swap, a remove", func(_ *testing.T, _ *os.Root, _, log string) error { return os.Remove(log) }, nil},
		{"the third swap, a rename over the name", func(_ *testing.T, _ *os.Root, dir, log string) error {
			return os.Rename(filepath.Join(dir, "other"), log)
		}, nil},
		{"the third swap, a rename over the name beneath the root", func(_ *testing.T, root *os.Root, _, _ string) error {
			return root.Rename("other", name)
		}, nil},
		{"the third swap, a rename over the name of its own hard link, then a remove", again(func(_ *os.Root, dir, log string) error {
			return os.Rename(filepath.Join(dir, "again"), log)
		}), taken},
		{"the third swap, a rename over the name of its own hard link beneath the root, then a remove", again(func(root *os.Root, _, _ string) error {
			return root.Rename("again", name)
		}), taken},
		{"the third swap, a hard link made over the name with replace", func(_ *testing.T, _ *os.Root, dir, log string) error {
			return linkOver(filepath.Join(dir, "other"), log, false)
		}, replaced},
		{"the third swap, a hard link made over the name with replace and POSIX semantics", func(_ *testing.T, _ *os.Root, dir, log string) error {
			return linkOver(filepath.Join(dir, "other"), log, true)
		}, replaced},
	}
	for _, row := range rows {
		t.Run(row.row, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			log, target := filepath.Join(dir, name), filepath.Join(dir, "target")
			put0(t, log, "")
			put0(t, target, "theirs")
			put0(t, filepath.Join(dir, "other"), "")
			swaps := map[string]func() error{
				"look": func() error { return errors.Join(os.Remove(log), os.Symlink("target", log)) },
				"held": func() error { return errors.Join(os.Remove(log), os.Link(target, log)) },
			}
			if row.checked != nil {
				judge := func(_ *testing.T, act string, err error) error { return held(act, err) }
				if row.judge != nil {
					judge = row.judge
				}
				swaps["checked"] = func() error { return judge(t, row.row, row.checked(t, root, dir, log)) }
			}
			err = appendWith(t.Context(), root, name, []byte("line"), 100, settle([]Option{asideWith(name + ".1"), stepWith(func(at string) {
				if swap, ok := swaps[at]; ok {
					delete(swaps, at)
					if err := swap(); err != nil {
						t.Fatalf("the swap at %s: %v", at, err)
					}
				}
			})}))
			if err == nil || len(swaps) != 0 {
				t.Errorf("swaps left %v: %v, and want it refused", swaps, err)
			}
			holds(t, target, "theirs")
		})
	}
}

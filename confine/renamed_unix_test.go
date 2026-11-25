//go:build linux || darwin

package confine

import (
	"errors"
	"io/fs"
	"testing"

	"golang.org/x/sys/unix"
)

// Each answer of the system's rename is the error it means: a name that is there is fs.ErrExist,
// naming the name that is taken; a
// kernel or file system that does not take the flag is ErrNoExchange for a swap and
// errors.ErrUnsupported, not ErrNoExchange, for a move; and any other answer is itself. None is
// ErrOutside. A real file system with no swap, a FAT image, is held by hand (the pull request
// names the run).
//
// What it costs when this fails: a program that falls back from a swap to two renames cannot tell
// that it should, or a taken name reads as a failure to retry.
func TestEachAnswerOfTheRenameIsTheErrorItMeans(t *testing.T) {
	t.Parallel()
	cases := []struct {
		what     string
		errno    unix.Errno
		exchange bool
		want     error
	}{
		{"a name that is there", unix.EEXIST, false, fs.ErrExist},
		{"no swap here", unix.EOPNOTSUPP, true, ErrNoExchange},
		{"no move without replacing here", unix.EOPNOTSUPP, false, errors.ErrUnsupported},
		{"a denial", unix.EACCES, false, fs.ErrPermission},
	}
	for _, c := range cases {
		err := renamed(c.errno, "a", "b", c.exchange)
		if !errors.Is(err, c.want) || errors.Is(err, ErrOutside) {
			t.Errorf("%s: %v, want %v", c.what, err, c.want)
		}
	}
	if len(cases) != 4 {
		t.Fatalf("held %d answers", len(cases))
	}
	if pe, ok := errors.AsType[*fs.PathError](renamed(unix.EEXIST, "a", "b", false)); !ok || pe.Path != "b" {
		t.Errorf("a name that is there: %v, want it to name b, the name that is taken", pe)
	}
	if err := renamed(unix.EOPNOTSUPP, "a", "b", false); errors.Is(err, ErrNoExchange) {
		t.Errorf("a move with no flag: %v matches ErrNoExchange", err)
	}
	if err := renamed(nil, "a", "b", true); err != nil {
		t.Errorf("no error: %v", err)
	}
}

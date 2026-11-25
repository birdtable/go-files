//go:build unix

package confine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/birdtable/go-files/confine"
)

// What it costs when this fails: a program is told a file runs by a bit that does not apply to
// its account, an owner by the group's or other's, or one in the group by other's, or root is told
// a file with an exec bit does not run, or on a 32-bit build an owner past uid 2^31 is taken for
// another.
func TestIsRunnableTakesTheExecBitThatAppliesToThisAccount(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name string
		perm fs.FileMode
		uid  uint32
		gid  uint32
		me   int
		want bool
	}{
		{"the owner's bit", 0o100, 1000, 50, 1000, true},
		{"the owner, by others' bits", 0o011, 1000, 50, 1000, false},
		{"the group's bit", 0o010, 2000, 50, 1000, true},
		{"a supplementary group's bit", 0o010, 2000, 60, 1000, true},
		{"the group, by others' bit", 0o101, 2000, 50, 1000, false},
		{"another's bit", 0o001, 2000, 70, 1000, true},
		{"another, by the owner's and the group's", 0o110, 2000, 70, 1000, false},
		{"root, by others' bit", 0o001, 2000, 70, 0, true},
		{"root, by the owner's bit of another's file", 0o100, 2000, 70, 0, true},
		{"root, by no bit", 0o644, 2000, 70, 0, false},
		{"the owner past 2^31, as a 32-bit build's euid", 0o700, 4294967294, 50, -2, true},
		{"another past 2^31, by the owner's bit", 0o700, 4294967293, 50, -2, false},
	} {
		if got := confine.ExecBit(row.perm, row.uid, row.gid, row.me, []int{50, 60}); got != row.want {
			t.Errorf("%s: %v with %v, and want %v", row.name, got, row.perm, row.want)
		}
	}
	in := t.TempDir()
	root := openRoot(t, in)
	asRoot := os.Geteuid() == 0
	for _, row := range []struct {
		perm    fs.FileMode
		want    bool
		refused bool
	}{
		{0o700, true, false},
		{0o600, false, false},
		{0o671, asRoot, false},
		{0o111, asRoot, !asRoot},
	} {
		name := row.perm.String()
		writeMode(t, filepath.Join(in, name), row.perm)
		got, err := confine.IsRunnable(root, name)
		if got != row.want || row.refused != errors.Is(err, fs.ErrPermission) || !row.refused && err != nil {
			t.Errorf("a file %v of this account's, root %v: %v, %v, and want %v, refused %v", row.perm, asRoot, got, err, row.want, row.refused)
		}
	}
}

// What it costs when this fails: a pipe at the name holds IsRunnable until a writer comes, so a
// program listing its store hangs on one a hostile process left there.
func TestIsRunnableAnswersAPipeFalseWithoutWaiting(t *testing.T) {
	t.Parallel()
	in := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(in, "p.exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := confine.IsRunnable(openRoot(t, in), "p.exe")
	if got || err != nil {
		t.Errorf("a pipe 0755: %v, %v, and want false and no error", got, err)
	}
}

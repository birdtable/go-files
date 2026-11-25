//go:build unix

package trust

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// On this disk a folder of Who.Mounts is judged by Path, Held and ReadOwned alike, before the
// name: /dev, a mount's root under root's /, passes; the test's own folder, under a folder of
// this account's and no mount's root, is refused with ErrMountEntry, and so is a relative entry.
//
// What it costs when this fails: one of the three lends a mount's pass by an entry the others
// refuse, so a program held open, or a key read, passes on a disk another account controls.
func TestPathHeldAndReadOwnedEachJudgeWhoMountsOnThisDisk(t *testing.T) {
	t.Parallel()
	d := realDir(t)
	p := filepath.Join(d, "f")
	mk(t, p, Key, 0o600)
	me := "uid:" + strconv.Itoa(os.Geteuid())
	cases := []struct {
		name   string
		mounts []string
		want   error
	}{
		{"/dev, a mount's root under root's /", []string{"/dev"}, nil},
		{"the test's own folder, under this account's and no mount's root", []string{d}, ErrMountEntry},
		{"a relative entry", []string{"dev"}, ErrMountEntry},
	}
	for _, c := range cases {
		who := Who{Mounts: c.mounts}
		_, perr := Path(p, Key, who)
		herr := Held(hold(t, d, "f"), Key, who)
		_, rerr := ReadOwned(p, me, true, 16, who)
		for _, got := range []struct {
			by  string
			err error
		}{{"Path", perr}, {"Held", herr}, {"ReadOwned", rerr}} {
			if c.want == nil && got.err != nil || c.want != nil && !errors.Is(got.err, c.want) {
				t.Errorf("%s by %s: %v, and want %v", c.name, got.by, got.err, c.want)
			}
		}
	}
}

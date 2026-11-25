package confine_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// What it costs when this fails: on Windows a program is told README.md runs, as exec.LookPath
// tells it, or a name with no extension runs because foo.exe sits beside it, or a short name, or a
// trailing dot or space, answers otherwise than the long name the system runs.
func TestIsRunnableOnWindowsTakesTheExtensionPathextListsOfTheLongName(t *testing.T) {
	for _, row := range []struct {
		final, pathext string
		want           bool
	}{
		{`\a\x.exe`, "", true},
		{`\a\X.EXE`, "", true},
		{`\a\x.cmd`, "", true},
		{`\a\notes.md`, "", false},
		{`\a\foo`, "", false},
		{`\a\x.`, "", false},
		{`\a.exe\foo`, "", false},
		{`\a\notes.md`, ".COM;.EXE;.MD", true},
		{`\a\notes.md`, "md", true},
		{`\a\x.exe`, ".MD", false},
		{`\a\x.exefoo`, "", false},
	} {
		if got := confine.Listed(row.final, row.pathext); got != row.want {
			t.Errorf("%s with PATHEXT %q: %v, and want %v", row.final, row.pathext, got, row.want)
		}
	}
	t.Setenv("PATHEXT", "")
	in := t.TempDir()
	for _, name := range []string{"x.exe", "Y.EXE", "notes.md", "foo", "foo.exe", "x.exefoo"} {
		writeMode(t, filepath.Join(in, name), 0o644)
	}
	short := filepath.Base(shortName(t, filepath.Join(in, "x.exefoo")))
	root := openRoot(t, in)
	for _, row := range []struct {
		name string
		want bool
	}{
		{"x.exe", true},
		{"Y.EXE", true},
		{"y.exe", true},
		{"notes.md", false},
		{"foo", false},
		{short, false},
		{"x.exe.", true},
		{"x.exe ", true},
		{"notes.md.", false},
	} {
		got, err := confine.IsRunnable(root, row.name)
		if got != row.want || err != nil {
			t.Errorf("%q: %v, %v, and want %v and no error", row.name, got, err, row.want)
		}
	}
	t.Setenv("PATHEXT", ".MD")
	if got, err := confine.IsRunnable(root, "notes.md"); !got || err != nil {
		t.Errorf("notes.md with PATHEXT .MD: %v, %v, and want true", got, err)
	}
	if got, err := confine.IsRunnable(root, "x.exe"); got || err != nil {
		t.Errorf("x.exe with PATHEXT .MD: %v, %v, and want false", got, err)
	}
}

// What it costs when this fails: a junction in the store, which any account makes with no
// privilege, has a program outside it answered runnable as the store's.
func TestIsRunnableOnWindowsRefusesAJunctionOnTheWay(t *testing.T) {
	t.Parallel()
	in, out := t.TempDir(), t.TempDir()
	writeMode(t, filepath.Join(out, "tool.exe"), 0o755)
	plantJunction(t, out, filepath.Join(in, "j"))
	got, err := confine.IsRunnable(openRoot(t, in), filepath.Join("j", "tool.exe"))
	if got || !errors.Is(err, confine.ErrOutside) {
		t.Errorf(`j\tool.exe through a junction: %v, %v, and want false and ErrOutside`, got, err)
	}
	if _, err := os.Stat(filepath.Join(in, "j", "tool.exe")); err != nil {
		t.Errorf("the junction does not lead to the program, so the row proves nothing: %v", err)
	}
}

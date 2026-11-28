package confine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// finalShortName is path's 8.3 short name, or path itself where the volume makes none.
func finalShortName(t *testing.T, path string) string {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetShortPathName(p, &buf[0], uint32(len(buf)))
	if err != nil {
		t.Fatal(err)
	}
	return windows.UTF16ToString(buf[:n])
}

func TestOnWindowsFinalAnswersTheLongStoredNameForAShortNameAndACase(t *testing.T) {
	t.Parallel()
	raw := t.TempDir()
	tmp := finalOK(t, raw)
	if strings.HasPrefix(tmp, `\\?\`) {
		t.Errorf("Final of the temp folder kept the \\\\?\\ prefix: %q", tmp)
	}
	want := filepath.Join(tmp, "A Long Mixed Case Folder")
	if err := os.Mkdir(want, 0o700); err != nil {
		t.Fatal(err)
	}
	short := finalShortName(t, want)
	if short == want {
		t.Fatalf("the temp folder's volume makes no 8.3 short name for %s, so no short name is tried", want)
	}
	for _, path := range []string{short, strings.ToUpper(want), strings.ToLower(short), filepath.Join(raw, "a long mixed case folder"), filepath.Join(finalShortName(t, raw), filepath.Base(short))} {
		if got := finalOK(t, path); got != want {
			t.Errorf("Final(%q) = %q, and want the long stored name %q", path, got, want)
		}
	}
}

func TestOnWindowsFinalKeepsThePrefixWhereTheNameReadsOtherwiseWithout(t *testing.T) {
	t.Parallel()
	tmp := finalTemp(t)
	dotted := `\\?\` + filepath.Join(tmp, "dotted.")
	if err := os.Mkdir(dotted, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := finalOK(t, dotted); got != dotted {
		t.Errorf("Final of a folder whose name ends in a dot = %q, and want %q, with the prefix", got, dotted)
	}
	long := tmp
	for len(long) < maxPath {
		long = filepath.Join(long, strings.Repeat("x", 40))
	}
	if err := os.MkdirAll(long, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, want := finalOK(t, `\\?\`+long), `\\?\`+long; got != want {
		t.Errorf("Final of a folder %d long = %q, and want %q, with the prefix", len(long), got, want)
	}
}

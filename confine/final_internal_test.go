package confine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// finalTemp is a temp folder named as Final names it, so names below it compare by bytes.
func finalTemp(t *testing.T) string {
	t.Helper()
	tmp, err := Final(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return tmp
}

// finalOK is Final's answer for path, failing the test on an error.
func finalOK(t *testing.T, path string) string {
	t.Helper()
	got, err := Final(path)
	if err != nil {
		t.Fatalf("Final(%q): %v", path, err)
	}
	return got
}

func TestFinalAnswersOneNameForEachSpellingThroughALinkedParent(t *testing.T) {
	t.Parallel()
	tmp := finalTemp(t)
	want := filepath.Join(tmp, "Real", "MixedCase")
	if err := os.MkdirAll(want, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(want, "File.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	plantFolderLink(t, tmp, "Real", "via")
	spellings := []string{
		want,
		filepath.Join(tmp, "via", "MixedCase"),
		tmp + string(filepath.Separator) + "Real" + string(filepath.Separator) + "." + string(filepath.Separator) + "MixedCase",
		t.TempDir() + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(tmp) + string(filepath.Separator) + filepath.Join("Real", "MixedCase"),
	}
	for _, path := range spellings {
		if got := finalOK(t, path); got != want {
			t.Errorf("Final(%q) = %q, and want %q", path, got, want)
		}
		if got := finalOK(t, filepath.Join(path, "File.txt")); got != filepath.Join(want, "File.txt") {
			t.Errorf("Final of File.txt in %q = %q, and want %q", path, got, filepath.Join(want, "File.txt"))
		}
	}
	if got := finalOK(t, want); finalOK(t, got) != got {
		t.Errorf("Final of its own answer %q is another name", got)
	}
}

func TestFinalAnswersTheStoredCaseWhereTheVolumeFoldsCase(t *testing.T) {
	t.Parallel()
	tmp := finalTemp(t)
	folds, foldsErr := Folds(tmp)
	want := filepath.Join(tmp, "MixedCase")
	if err := os.Mkdir(want, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := finalOK(t, want); got != want {
		t.Errorf("Final(%q) = %q, and want it as written", want, got)
	}
	for _, path := range []string{filepath.Join(tmp, "mixedcase"), filepath.Join(tmp, "MIXEDCASE"), filepath.Join(strings.ToUpper(tmp), "mIxEdCaSe")} {
		got, err := Final(path)
		switch {
		case foldsErr == nil && folds.FoldsCase():
			if err != nil || got != want {
				t.Errorf("Final(%q) on a volume that folds case = %q, %v, and want the stored case %q", path, got, err, want)
			}
		case foldsErr == nil:
			if !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("Final(%q) on a volume that tells case apart = %q, %v, and want fs.ErrNotExist", path, got, err)
			}
		case err == nil && got != want:
			t.Errorf("Final(%q) where Folds cannot say (%v) = %q, and want the stored case %q or fs.ErrNotExist", path, foldsErr, got, want)
		}
	}
}

func TestFinalAnswersALinkAtTheLastNameItself(t *testing.T) {
	t.Parallel()
	tmp := finalTemp(t)
	if err := os.Mkdir(filepath.Join(tmp, "target"), 0o700); err != nil {
		t.Fatal(err)
	}
	plantFolderLink(t, tmp, "target", "link")
	if got, want := finalOK(t, filepath.Join(tmp, "link")), filepath.Join(tmp, "link"); got != want {
		t.Errorf("Final of the link = %q, and want the link's own name %q", got, want)
	}
	resolved, err := Resolve(filepath.Join(tmp, "link"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := finalOK(t, resolved), filepath.Join(tmp, "target"); got != want {
		t.Errorf("Final of the link resolved first = %q, and want the target %q", got, want)
	}
	if err := os.Remove(filepath.Join(tmp, "target")); err != nil {
		t.Fatal(err)
	}
	if got, want := finalOK(t, filepath.Join(tmp, "link")), filepath.Join(tmp, "link"); got != want {
		t.Errorf("Final of the link to nothing = %q, and want the link's own name %q", got, want)
	}
}

func TestUndeviceDropsThePrefixOnlyWhereTheNameReadsTheSameWithout(t *testing.T) {
	t.Parallel()
	// full is GetFullPathName's rule for the names below: a last name's trailing dots and spaces
	// stripped, and CON read as the console.
	full := func(p string) (string, error) {
		switch {
		case strings.HasSuffix(p, `\CON`):
			return `\\.\CON`, nil
		case strings.HasSuffix(p, `\fails`):
			return "", errors.New("GetFullPathName failed")
		}
		return strings.TrimRight(p, ". "), nil
	}
	long := `C:\` + strings.Repeat("x", maxPath-len(`C:\`))
	for name, want := range map[string]string{
		`\\?\C:\Users\runneradmin\a`:      `C:\Users\runneradmin\a`,
		`\\?\c:\a`:                        `c:\a`,
		`\\?\C:\`:                         `C:\`,
		`\\?\UNC\host\share\a`:            `\\host\share\a`,
		`\\?\C:\a\dotted.`:                `\\?\C:\a\dotted.`,
		`\\?\C:\a\spaced `:                `\\?\C:\a\spaced `,
		`\\?\C:\a\CON`:                    `\\?\C:\a\CON`,
		`\\?\C:\a\fails`:                  `\\?\C:\a\fails`,
		`\\?\` + long:                     `\\?\` + long,
		`\\?\` + long[:len(long)-1]:       long[:len(long)-1],
		`\\?\Volume{0123-4567}\a`:         `\\?\Volume{0123-4567}\a`,
		`\\?\GLOBALROOT\Device\Harddisk0`: `\\?\GLOBALROOT\Device\Harddisk0`,
		`\\?\1:\a`:                        `\\?\1:\a`,
		`C:\a`:                            `C:\a`,
	} {
		if got := undevice(name, full); got != want {
			t.Errorf("undevice(%q) = %q, and want %q", name, got, want)
		}
	}
}

func TestFinalRefusesARelativePathAndANameThatIsNotThere(t *testing.T) {
	t.Parallel()
	_, err := Final(filepath.Join("a", "b"))
	if !errors.Is(err, fs.ErrInvalid) || !errors.Is(err, ErrOutside) {
		t.Errorf("Final of a relative path: %v, and want fs.ErrInvalid and ErrOutside", err)
	}
	missing := filepath.Join(finalTemp(t), "missing")
	_, err = Final(missing)
	if pe, ok := errors.AsType[*fs.PathError](err); !ok || pe.Path != missing || !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Final of a missing name: %v, and want an *fs.PathError naming it, matching fs.ErrNotExist", err)
	}
	if errors.Is(err, errors.ErrUnsupported) || errors.Is(err, ErrOutside) {
		t.Errorf("Final of a missing name: %v, and want neither errors.ErrUnsupported nor ErrOutside", err)
	}
}

func TestFinalRefusesANameThatIsNotTheFileOpened(t *testing.T) {
	t.Parallel()
	tmp := finalTemp(t)
	file, other := filepath.Join(tmp, "file"), filepath.Join(tmp, "other")
	for _, p := range []string{file, other} {
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, answered := range []string{other, filepath.Join(tmp, "gone"), file + " (deleted)"} {
		_, err := final(file, func(*os.File) (string, error) { return answered, nil })
		if !errors.Is(err, ErrChanged) || !errors.Is(err, ErrOutside) {
			t.Errorf("Final where the system answers %q: %v, and want ErrChanged and ErrOutside", answered, err)
		}
	}
	if got, err := final(file, func(*os.File) (string, error) { return file, nil }); err != nil || got != file {
		t.Errorf("Final where the system answers the file's own name = %q, %v, and want it with no error", got, err)
	}
}

func TestFinalRefusesAnAnswerTheSystemCannotGiveAsUnsupported(t *testing.T) {
	t.Parallel()
	file := filepath.Join(finalTemp(t), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for why, read := range map[string]func(*os.File) (string, error){
		"a call that fails with fs.ErrNotExist": func(*os.File) (string, error) {
			return "", &fs.PathError{Op: "readlink", Path: "/proc/self/fd/3", Err: fs.ErrNotExist}
		},
		"a relative name": func(*os.File) (string, error) { return "anon_inode:[eventfd]", nil },
		"an empty name":   func(*os.File) (string, error) { return "", nil },
	} {
		_, err := final(file, read)
		if !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("Final where the system answers %s: %v, and want errors.ErrUnsupported", why, err)
		}
		if errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Final where the system answers %s: %v, and want no match of fs.ErrNotExist, as the file is there", why, err)
		}
	}
}

func TestFinalOfAFileWithASecondNameAnswersTheNameOpenedOrOnMacOSIsRefused(t *testing.T) {
	t.Parallel()
	tmp := finalTemp(t)
	a, b := filepath.Join(tmp, "a"), filepath.Join(tmp, "sub", "b")
	if err := os.WriteFile(a, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Dir(b), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(a, b); err != nil {
		t.Fatal(err)
	}
	// look has another look at b between the open of a and the read, as any process may.
	look := func(f *os.File) (string, error) {
		if _, err := os.Lstat(b); err != nil {
			return "", err
		}
		return finalOf(f)
	}
	for _, p := range []string{a, b} {
		got, err := final(p, look)
		if runtime.GOOS == "darwin" {
			if !errors.Is(err, errors.ErrUnsupported) || got != "" {
				t.Errorf("Final on macOS of %s, a file with a second name, while %s is looked at = %q, %v, and want errors.ErrUnsupported", p, b, got, err)
			}
			continue
		}
		if err != nil || got != p {
			t.Errorf("Final of %s, a file with a second name, while %s is looked at = %q, %v, and want the name opened", p, b, got, err)
		}
	}
	if got := finalOK(t, filepath.Dir(b)); got != filepath.Dir(b) {
		t.Errorf("Final of the folder %s = %q, and want its own name", filepath.Dir(b), got)
	}
}

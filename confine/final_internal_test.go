package confine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

// finalFile checks Final's answer for path, which is no folder: want, or, where Final answers only
// a folder, as on macOS, errors.ErrUnsupported and no name.
func finalFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := Final(path)
	if finalFoldersOnly {
		if !errors.Is(err, errors.ErrUnsupported) || got != "" {
			t.Errorf("Final of %s, no folder, here = %q, %v, and want errors.ErrUnsupported", path, got, err)
		}
		return
	}
	if err != nil || got != want {
		t.Errorf("Final(%q) = %q, %v, and want %q", path, got, err, want)
	}
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
		finalFile(t, filepath.Join(path, "File.txt"), filepath.Join(want, "File.txt"))
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
	finalFile(t, filepath.Join(tmp, "link"), filepath.Join(tmp, "link"))
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
	finalFile(t, filepath.Join(tmp, "link"), filepath.Join(tmp, "link"))
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
	dir, other := filepath.Join(tmp, "dir"), filepath.Join(tmp, "other")
	for _, p := range []string{dir, other} {
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, answered := range []string{other, filepath.Join(tmp, "gone"), dir + " (deleted)"} {
		_, err := final(dir, func(*os.File) (string, error) { return answered, nil }, os.Lstat)
		if !errors.Is(err, ErrChanged) || !errors.Is(err, ErrOutside) {
			t.Errorf("Final where the system answers %q: %v, and want ErrChanged and ErrOutside", answered, err)
		}
	}
	if got, err := final(dir, func(*os.File) (string, error) { return dir, nil }, os.Lstat); err != nil || got != dir {
		t.Errorf("Final where the system answers the folder's own name = %q, %v, and want it with no error", got, err)
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
		_, err := final(file, read, os.Lstat)
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
		got, err := final(p, look, os.Lstat)
		if finalFoldersOnly {
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

// What it costs when this fails: the race test's window, held open: a second name b of the file
// is linked and looked at, so macOS's F_GETPATH answers it; unlinked before the open file is read,
// so a count of its names there is 1; and linked again before b is looked at, so os.SameFile
// passes it. Final answers b, a name in another folder than the one opened, and a journal keys one
// file by two names.
func TestFinalOfAFileWhoseSecondNameIsUnlinkedAndLinkedBetweenItsReadsAnswersNoOtherName(t *testing.T) {
	t.Parallel()
	tmp := finalTemp(t)
	a, b := filepath.Join(tmp, "a"), filepath.Join(tmp, "sub", "b")
	if err := os.WriteFile(a, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Dir(b), 0o700); err != nil {
		t.Fatal(err)
	}
	var answered string
	read := func(f *os.File) (string, error) {
		if err := os.Link(a, b); err != nil {
			return "", err
		}
		if _, err := os.Lstat(b); err != nil {
			return "", err
		}
		name, err := finalOf(f)
		answered = name
		if err := os.Remove(b); err != nil {
			return "", err
		}
		return name, err
	}
	lstat := func(name string) (fs.FileInfo, error) {
		if err := os.Link(a, b); err != nil {
			return nil, err
		}
		return os.Lstat(name)
	}
	got, err := final(a, read, lstat)
	if !finalFoldersOnly {
		if err != nil || got != a {
			t.Errorf("Final(%s), its second name unlinked and linked again between its reads = %q, %v, and want the name opened", a, got, err)
		}
		return
	}
	if answered != b {
		t.Fatalf("F_GETPATH answered %q, and the test wants %s, the name looked at last, so the window is open", answered, b)
	}
	if !errors.Is(err, errors.ErrUnsupported) || got != "" {
		t.Errorf("Final on macOS of %s, its second name %s unlinked and linked again between its reads = %q, %v, and want errors.ErrUnsupported", a, b, got, err)
	}
}

// finalRaceCalls is how many times each race test calls Final.
const finalRaceCalls = 200_000

// finalRace calls Final on p finalRaceCalls times while another goroutine runs race over and over,
// and answers how many calls were refused with an error refused passes. It fails the test on any
// other error, on any name but p, and where race, which answers whether it did what it is for,
// never once did.
func finalRace(t *testing.T, p string, race func() bool, refused func(error) bool) int {
	t.Helper()
	stop, stopped := make(chan struct{}), make(chan struct{})
	var ran atomic.Int64
	go func() {
		defer close(stopped)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if race() {
				ran.Add(1)
			}
		}
	}()
	n, wrong := 0, map[string]int{}
	for range finalRaceCalls {
		got, err := Final(p)
		switch {
		case err == nil && got != p:
			wrong[got]++
		case err != nil && refused(err):
			n++
		case err != nil:
			t.Errorf("Final(%s) under the race: %v", p, err)
		}
	}
	close(stop)
	<-stopped
	t.Logf("%d calls under a race that ran %d times: %d refused, %d wrong names", finalRaceCalls, ran.Load(), n, len(wrong))
	if ran.Load() == 0 {
		t.Errorf("the race on %s never once ran, so the test tells nothing", p)
	}
	for name, k := range wrong {
		t.Errorf("Final(%s) under the race answered %s %d times, and want no name but its own", p, name, k)
	}
	return n
}

// What it costs when this fails: on macOS, another process links a second name of a file in another
// folder, looks at it and unlinks it, over and over, and one of Final's answers is that second
// name, which F_GETPATH answered while it was there and no count of names tells: a journal keys one
// file by two names. So macOS refuses every call, as a file is no folder, and Linux and Windows
// answer the name opened every time.
func TestFinalUnderALinkLookUnlinkRaceAnswersNoOtherName(t *testing.T) {
	t.Parallel()
	tmp := finalTemp(t)
	a, b := filepath.Join(tmp, "a"), filepath.Join(tmp, "sub", "b")
	if err := os.WriteFile(a, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Dir(b), 0o700); err != nil {
		t.Fatal(err)
	}
	refused := finalRace(t, a, func() bool {
		if os.Link(a, b) != nil {
			return false
		}
		_, _ = os.Lstat(b)
		_ = os.Remove(b)
		return true
	}, func(err error) bool { return finalFoldersOnly && errors.Is(err, errors.ErrUnsupported) })
	if finalFoldersOnly && refused != finalRaceCalls {
		t.Errorf("Final(%s), a file, under the race on macOS was refused %d times of %d, and want every one", a, refused, finalRaceCalls)
	}
}

// What it costs when this fails: on macOS, another process looks at a folder by other names, in
// another case and through a link to its parent, and tries to give it a second name by link(2),
// over and over, and one of Final's answers is a name the folder is not stored by: a journal keys
// one folder by two names.
func TestFinalOfAFolderUnderARaceOfLooksByOtherNamesAnswersNoOtherName(t *testing.T) {
	t.Parallel()
	tmp := finalTemp(t)
	dir := filepath.Join(tmp, "sub", "Folder")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tmp, "other"), 0o700); err != nil {
		t.Fatal(err)
	}
	plantFolderLink(t, tmp, "sub", "via")
	upper, via, second := filepath.Join(tmp, "sub", "FOLDER"), filepath.Join(tmp, "via", "Folder"), filepath.Join(tmp, "other", "b")
	finalRace(t, dir, func() bool {
		_, errUpper := os.Lstat(upper)
		_, errVia := os.Lstat(via)
		if os.Link(dir, second) == nil {
			_, _ = os.Lstat(second)
			_ = os.Remove(second)
		}
		return errUpper == nil || errVia == nil
	}, func(error) bool { return false })
}

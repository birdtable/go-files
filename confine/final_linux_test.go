package confine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// finalRefusesASecondName is whether Final refuses a file with a second name here: Linux answers a file with a second name by the name it was opened by.
const finalRefusesASecondName = false

func TestOnLinuxFinalAnswersANameThisAccountMayNotRead(t *testing.T) {
	t.Parallel()
	tmp := finalTemp(t)
	file, folder := filepath.Join(tmp, "file"), filepath.Join(tmp, "folder")
	if err := os.WriteFile(file, nil, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(folder, 0o300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(folder, 0o700) })
	for _, p := range []string{file, folder} {
		if got := finalOK(t, p); got != p {
			t.Errorf("Final of %s, which this account may not read, = %q, and want its own name", p, got)
		}
	}
}

func TestOnLinuxFinalWithNoProcIsErrUnsupported(t *testing.T) {
	t.Parallel()
	tmp := finalTemp(t)
	noProc := filepath.Join(tmp, "proc", "self", "fd")
	_, err := final(tmp, func(f *os.File) (string, error) { return procName(noProc, f) })
	if !errors.Is(err, errors.ErrUnsupported) || errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Final where /proc/self/fd is not there: %v, and want errors.ErrUnsupported and no match of fs.ErrNotExist", err)
	}
}

func TestOnLinuxFinalRefusesAProcSelfFdThatIsNotAProcfs(t *testing.T) {
	t.Parallel()
	tmp := finalTemp(t)
	fds := filepath.Join(tmp, "fds")
	if err := os.Mkdir(fds, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(tmp, filepath.Join(tmp, "via")); err != nil {
		t.Fatal(err)
	}
	// fds stands where a mount over /proc/self/fd would, its link <n> written to name the file
	// through a linked parent, which os.SameFile alone would pass.
	got, err := final(filepath.Join(tmp, "file"), func(f *os.File) (string, error) {
		if err := os.Symlink(filepath.Join(tmp, "via", "file"), filepath.Join(fds, strconv.FormatUint(uint64(f.Fd()), 10))); err != nil {
			return "", err
		}
		return procName(fds, f)
	})
	if !errors.Is(err, errors.ErrUnsupported) || got != "" {
		t.Errorf("Final where /proc/self/fd is a folder of another file system = %q, %v, and want errors.ErrUnsupported", got, err)
	}
}

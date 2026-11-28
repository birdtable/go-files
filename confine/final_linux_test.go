package confine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

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

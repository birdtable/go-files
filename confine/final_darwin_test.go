package confine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestOnMacOSFinalAnswersOneNameThroughTheDataVolumesFirmlink(t *testing.T) {
	t.Parallel()
	tmp := finalTemp(t)
	data := "/System/Volumes/Data" + tmp
	if got := finalOK(t, data); got != tmp {
		t.Errorf("Final(%q) = %q, and want the name through the firmlink %q", data, got, tmp)
	}
}

func TestOnMacOSFinalOfANameThisAccountMayNotReadIsErrPermission(t *testing.T) {
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
		got, err := Final(p)
		if os.Geteuid() == 0 {
			if err != nil || got != p {
				t.Errorf("Final of %s as root = %q, %v, and want its own name", p, got, err)
			}
			continue
		}
		if pe, ok := errors.AsType[*fs.PathError](err); !ok || pe.Path != p || !errors.Is(err, fs.ErrPermission) {
			t.Errorf("Final of %s, which this account may not read: %v, and want an *fs.PathError naming it, matching fs.ErrPermission", p, err)
		}
	}
}

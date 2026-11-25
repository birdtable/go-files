package confine

import (
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// pcCaseSensitive is pathconf's _PC_CASE_SENSITIVE, from macOS's sys/unistd.h: 1 where the volume
// tells names apart by case, 0 where it folds case.
const pcCaseSensitive = 11

// askFolding asks the volume of f, the folder root opened, its type and its case sensitivity.
func askFolding(f *os.File, root string) (Folding, error) {
	c, err := f.SyscallConn()
	if err != nil {
		return FoldUnknown, err
	}
	var st unix.Statfs_t
	sensitive := -1
	var serr, perr error
	if err := c.Control(func(fd uintptr) {
		if serr = unix.Fstatfs(int(fd), &st); serr == nil {
			sensitive, perr = unix.Fpathconf(int(fd), pcCaseSensitive)
		}
	}); err != nil {
		return FoldUnknown, err
	}
	if serr != nil {
		return FoldUnknown, &fs.PathError{Op: "fstatfs", Path: root, Err: serr}
	}
	if perr != nil {
		perr = &fs.PathError{Op: "fpathconf", Path: root, Err: perr}
	}
	return darwinFolding(root, unix.ByteSliceToString(st.Fstypename[:]), sensitive, perr)
}

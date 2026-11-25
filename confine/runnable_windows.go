package confine

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// runs is whether the name the handle of f gives, normalized, the long name, ends in an extension
// PATHEXT lists. A short name, X~1.EXE for x.exefoo, and a trailing dot or space the open stripped,
// answer as that name does.
func runs(f *os.File, info fs.FileInfo) (bool, error) {
	const volumeNameNone = 0x4 // VOLUME_NAME_NONE with FILE_NAME_NORMALIZED (0), which x/sys does not name: no drive is asked for
	size := uint32(windows.MAX_PATH)
	for {
		buf := make([]uint16, size)
		n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buf[0], size, volumeNameNone)
		if err != nil {
			return false, &fs.PathError{Op: "GetFinalPathNameByHandle", Path: info.Name(), Err: err}
		}
		if n < size {
			return listed(windows.UTF16ToString(buf[:n]), os.Getenv("PATHEXT")), nil //nolint:forbidigo // the system's list of what runs, not configuration
		}
		size = n
	}
}

// listed is whether the extension of the file name final is one of pathext's, split by
// semicolons, compared without case, or of Go's own list where pathext is empty: the Windows
// rule, as data. An entry without its dot is taken with one, as exec.LookPath takes it.
func listed(final, pathext string) bool {
	ext := filepath.Ext(final)
	if ext == "" || ext == "." {
		return false
	}
	if pathext == "" {
		pathext = ".com;.exe;.bat;.cmd"
	}
	for e := range strings.SplitSeq(pathext, ";") {
		if e != "" && !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		if strings.EqualFold(e, ext) {
			return true
		}
	}
	return false
}

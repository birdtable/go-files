package trusttest

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// candidates is ntdll.dll in the folder GetSystemDirectory answers, which no variable of the
// environment moves, as %SystemRoot% would.
func candidates() ([]string, error) {
	dir, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, err
	}
	return []string{filepath.Join(dir, "ntdll.dll")}, nil
}

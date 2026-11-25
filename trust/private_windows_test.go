//go:build windows

package trust

import (
	"testing"

	"golang.org/x/sys/windows"
)

// judgedDir is a folder of the test's own that Path passes as a Folder, with no link in it.
func judgedDir(t *testing.T) string { t.Helper(); return nativeDir(t) }

// plantFolderLink makes at a junction to the folder target, which need not be there.
func plantFolderLink(t *testing.T, at, target string) { t.Helper(); junction(t, at, target) }

// openToOthers lets Everyone read the folder p, beside this account in full.
func openToOthers(t *testing.T, p string) {
	t.Helper()
	setList(t, p, grant(me(t), full), grant(everyone, windows.FILE_GENERIC_READ))
}

// writableByOthers lets Everyone change the folder p, beside this account in full.
func writableByOthers(t *testing.T, p string) {
	t.Helper()
	setList(t, p, grant(me(t), full), grant(everyone, windows.FILE_GENERIC_WRITE))
}

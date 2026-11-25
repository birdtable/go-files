package confine_test

import (
	"testing"

	"golang.org/x/sys/windows"
)

// shortName is path's 8.3 short name, or path itself where the volume makes none.
func shortName(t *testing.T, path string) string {
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

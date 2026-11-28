//go:build !windows

package fileid

import "testing"

// TestNoPathIsADevicePathOffWindows: off Windows a backslash is a name's own character, so even
// \\.\pipe\x names a file, and DevicePath refuses none.
//
// What it costs when this fails: a Unix name made of backslashes is refused by trust, confine and
// appdirs as a Windows device.
func TestNoPathIsADevicePathOffWindows(t *testing.T) {
	t.Parallel()
	for _, p := range []string{`\\.\pipe\x`, `\\?\pipe\x`, `\??\pipe\x`} {
		if DevicePath(p) {
			t.Errorf("DevicePath(%q) = true off Windows", p)
		}
	}
}

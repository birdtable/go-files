package fileid

import "testing"

// TestADevicePathIsRefusedOnWindows: on Windows DevicePath is the rule itself, for \\.\, a \\?\
// that names no drive or volume, and \??\, and passes a drive, a \\?\ drive and a UNC path.
//
// What it costs when this fails: trust, confine and appdirs open a pipe a caller names, on the
// one system where a path can name one.
func TestADevicePathIsRefusedOnWindows(t *testing.T) {
	t.Parallel()
	for p, want := range map[string]bool{
		`\\.\pipe\x`:     true,
		`\\?\pipe\x`:     true,
		`\??\pipe\x`:     true,
		`C:\x`:           false,
		`\\?\C:\x`:       false,
		`\\host\share\x`: false,
	} {
		if got := DevicePath(p); got != want {
			t.Errorf("DevicePath(%q) = %v, want %v", p, got, want)
		}
	}
}

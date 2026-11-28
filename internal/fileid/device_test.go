package fileid

import "testing"

// TestADevicePathIsAnyNameUnderTheDevicePrefixesButADriveOrAVolume: Windows' device rule, held on
// every system: a path under \\.\, and one under \\?\ or \??\ that names neither a drive nor a
// Volume{GUID}, with either slash, is a device path; \\?\ and \??\ written with backslashes and
// followed by a drive or a volume, a UNC path, a drive path and a relative one are not.
//
// What it costs when this fails: trust, confine or appdirs opens \\.\pipe\x or \\?\pipe\x, and the
// pipe's server is made the client's server, or a \\?\ path with a drive is refused and a long
// path on a drive cannot be named.
func TestADevicePathIsAnyNameUnderTheDevicePrefixesButADriveOrAVolume(t *testing.T) {
	t.Parallel()
	const guid = "Volume{0a1b2c3d-4e5f-6789-abcd-ef0123456789}"
	cases := []struct {
		path string
		want bool
	}{
		{`\\.\pipe\x`, true},
		{`\\.\PIPE\x`, true},
		{`//./pipe/x`, true},
		{`\\.\C:\x`, true},
		{`\\.\` + guid + `\x`, true},
		{`\\.\UNC\host\share\x`, true},
		{`\\.\`, true},
		{`\\.`, true},
		{`\\?\pipe\x`, true},
		{`\??\pipe\x`, true},
		{`\\?\GLOBALROOT\Device\NamedPipe\x`, true},
		{`\\?\UNC\host\share\x`, true},
		{`\\?\`, true},
		{`\\?`, true},
		{`\??`, true},
		{`//?/C:/x`, true},
		{`/??/C:/x`, true},
		{`\/?\C:\x`, true},
		{`\\?\C:/x`, true},
		{`\\?\CC:\x`, true},
		{`\\?\1:\x`, true},
		{`\\?\C`, true},
		{`\\?\Volume{0a1b2c3d-4e5f-6789-abcd-ef012345678}\x`, true},
		{`\\?\Volume{0a1b2c3d-4e5f-6789-abcd-ef01234567890}\x`, true},
		{`\\?\Volume{0a1b2c3d_4e5f-6789-abcd-ef0123456789}\x`, true},
		{`\\?\Volume{0a1b2c3d-4e5f-6789-abcd-ef012345678g}\x`, true},
		{`\\?\Volume(0a1b2c3d-4e5f-6789-abcd-ef0123456789)\x`, true},
		{`\\?\C:\x`, false},
		{`\\?\c:\x`, false},
		{`\\?\Z:`, false},
		{`\??\C:\x`, false},
		{`\\?\` + guid + `\x`, false},
		{`\\?\VOLUME{0A1B2C3D-4E5F-6789-ABCD-EF0123456789}`, false},
		{`\??\` + guid + `\`, false},
		{`\\host\share\x`, false},
		{`\\host\pipe\x`, false},
		{`\\.host\share`, false},
		{`\\?host\share`, false},
		{`C:\x`, false},
		{`\x`, false},
		{`x`, false},
		{`\\`, false},
		{``, false},
		{`/tmp/x`, false},
	}
	ran := 0
	for _, c := range cases {
		if got := deviceName(c.path); got != c.want {
			t.Errorf("deviceName(%q) = %v, want %v", c.path, got, c.want)
		}
		ran++
	}
	if ran != len(cases) {
		t.Fatalf("ran %d of %d cases", ran, len(cases))
	}
}

package fileid

import "strings"

// DevicePath reports whether path, as Windows parses it, names a device or a pipe rather than a
// file on a drive or a volume: a path under \\.\, or under \\?\ or \??\ that names neither a drive
// (X:) nor a volume (Volume{GUID}), each written with either slash. Such a path opens no file
// system's file: \\.\pipe\x and \\?\pipe\x open a named pipe, whose server the open makes a client
// of, and \\?\GLOBALROOT\..., \\?\UNC\... and \\.\C:\... reach a device or a share by a name trust,
// confine and appdirs do not read as a path. Only \\?\ and \??\ written with backslashes alone, as
// the system takes them literally, pass with a drive or a volume. A UNC path (\\host\share\...) is
// not one: it names a share, and its open connects to the host and authenticates the caller to it,
// which the caller refuses where it must. trust, confine and appdirs refuse a device path before
// anything is opened. Off Windows no path is one: a backslash there is a name's own character.
func DevicePath(path string) bool { return deviceNames && deviceName(path) }

// deviceName is DevicePath's rule on every system, so a test of it runs on each.
func deviceName(path string) bool {
	if len(path) < 3 || !sep(path[0]) || len(path) > 3 && !sep(path[3]) {
		return false
	}
	if !devicePrefix(path) {
		return false
	}
	if len(path) == 3 || path[:4] != `\\?\` && path[:4] != `\??\` {
		return true
	}
	head, _, _ := strings.Cut(path[4:], `\`)
	return !drive(head) && !volume(head)
}

// devicePrefix is whether path, three bytes or more, starts \\. or \\? or \??, either slash
// standing for each backslash.
func devicePrefix(path string) bool {
	return sep(path[1]) && (path[2] == '.' || path[2] == '?') || path[1] == '?' && path[2] == '?'
}

// hexDigit is whether c is a hex digit, in either case.
func hexDigit(c byte) bool { return '0' <= c && c <= '9' || 'a' <= c|0x20 && c|0x20 <= 'f' }

// sep is whether c is a separator in a Windows path: a backslash or a slash.
func sep(c byte) bool { return c == '\\' || c == '/' }

// drive is whether s is a drive, one ASCII letter and a colon.
func drive(s string) bool {
	return len(s) == 2 && s[1] == ':' && ('a' <= s[0]|0x20 && s[0]|0x20 <= 'z')
}

// volume is whether s is a volume's name, Volume{GUID}, in any case, the GUID's 32 hex digits in
// its 8-4-4-4-12 groups.
func volume(s string) bool {
	const prefix = "Volume{"
	if len(s) != len(prefix)+36+1 || !strings.EqualFold(s[:len(prefix)], prefix) || s[len(s)-1] != '}' {
		return false
	}
	for i, c := range []byte(s[len(prefix) : len(s)-1]) {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !hexDigit(c) {
				return false
			}
		}
	}
	return true
}

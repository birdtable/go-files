package confine

import (
	"strings"
	"unicode/utf16"
)

// maxPath is Windows's MAX_PATH, in UTF-16 units with the closing NUL: a name this long or longer
// opens only by the \\?\ form, or where long paths are turned on.
const maxPath = 260

// undevice is name, a name GetFinalPathNameByHandle gave in the \\?\ form, without that prefix,
// \\?\C:\a as C:\a and \\?\UNC\host\share\a as \\host\share\a, where the name without it is
// shorter than MAX_PATH and full, GetFullPathName on Windows, gives it back unchanged: a name
// that ends in a dot or a space, which the Win32 rules strip, and a device name such as CON, which
// they read as the device, keep the prefix, as does any other form and a failed full. It is the
// rule as data, so it is held on every system.
func undevice(name string, full func(string) (string, error)) string {
	rest, ok := strings.CutPrefix(name, `\\?\`)
	if !ok {
		return name
	}
	var short string
	switch {
	case strings.HasPrefix(rest, `UNC\`):
		short = `\\` + rest[len(`UNC\`):]
	case len(rest) >= 3 && isDriveLetter(rest[0]) && rest[1] == ':' && rest[2] == '\\':
		short = rest
	default:
		return name
	}
	if len(utf16.Encode([]rune(short))) >= maxPath {
		return name
	}
	if got, err := full(short); err != nil || got != short {
		return name
	}
	return short
}

// isDriveLetter is whether c is an ASCII letter, as a drive is named.
func isDriveLetter(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

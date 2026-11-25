package filestest

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// AllPortable fails the test once, with t.Errorf, when a path of paths is not portable to every
// system or two of them meet where a folder folds case or a normal form. Each is a
// slash-separated local path: it goes through confine.Local, and each of its names through
// confine.Portable, and a name's refusal is said before Local's, so the message is the same on
// every system. Two paths collide when one's leading names and the other's, spelled apart, have
// equal keys one by one: a/README and a/readme as files, and A/x and a/y as folders A and a. The
// message names every path refused, with its rule, and every pair that collides, a line each in
// sorted order; with nothing to say it says nothing. A path given twice is one path.
func AllPortable(t testing.TB, paths ...string) {
	t.Helper()
	type first struct{ spelled, path string }
	byKey := map[string]first{}
	var said []string
	for _, p := range slices.Compact(slices.Sorted(slices.Values(paths))) {
		keys, err := portable(p)
		if err != nil {
			said = append(said, fmt.Sprintf("%q: %v", p, err))
			continue
		}
		names := strings.Split(p, "/")
		for i := range names {
			spelled, key := strings.Join(names[:i+1], "/"), strings.Join(keys[:i+1], "/")
			f, ok := byKey[key]
			if !ok {
				byKey[key] = first{spelled, p}
				continue
			}
			if f.spelled != spelled {
				said = append(said, fmt.Sprintf("%+q and %+q meet where a folder folds case or a normal form: %+q and %+q share the key %+q", f.path, p, f.spelled, spelled, key))
				break
			}
		}
	}
	if len(said) > 0 {
		slices.Sort(said)
		t.Errorf("filestest.AllPortable: not portable to every system:\n\t%s", strings.Join(said, "\n\t"))
	}
}

// portable is the key of each of p's names, or why p is not portable.
func portable(p string) ([]string, error) {
	names := strings.Split(p, "/")
	keys := make([]string, len(names))
	for i, name := range names {
		key, err := confine.Portable(name)
		if err != nil {
			return nil, err
		}
		keys[i] = key
	}
	if _, err := confine.Local(p); err != nil {
		return nil, err
	}
	return keys, nil
}

// HostileName is a name or a slash-separated path a program must refuse, and the rule that
// refuses it.
type HostileName struct {
	Path string // a name or a path a program must refuse
	Why  string // the rule that refuses it, in the library's words: confine's refusal says it
}

// HostileNames answers a fresh copy of the one list of hostile names and paths, the one confine's
// own tests read: drive letters, backslashes, rooted and climbing paths, the
// device names Windows keeps, a trailing dot and space, 8.3 short names, .git in any case and
// disguise, the format and default-ignorable code points, the HFS+ ignorable ones among them,
// the characters that fold to several, a file's fork on macOS, and at least one entry for each
// rule of confine.Portable and confine.Local. Each is refused by confine.Local or, a name at a
// time, by confine.Portable, with an error that says its Why, on every system. Most rows are
// go-git v5.19.3's own tests of .git and its 8.3 and NTFS and HFS+ disguises (Apache-2.0; NOTICE
// names it).
func HostileNames() []HostileName {
	var list []HostileName
	add := func(why string, paths ...string) {
		for _, p := range paths {
			list = append(list, HostileName{Path: p, Why: why})
		}
	}
	add("it is empty, . or ..", "", ".", "..", "/etc/passwd", "../etc/passwd", "a/../../b", "a//b", "a/")
	add("it is not UTF-8", "a\xffb", ".g\x80it")
	add("it is longer than 255 bytes", strings.Repeat("a", 256), strings.Repeat("\u00e9", 128))
	add(`it holds one of / \ : * ? " < > |`, "C:", "C:/Windows/System32", `C:\Windows`, `\\server\share`, `a\b`, "a:b", "a*b", "a?b", `a"b`,
		"a<b", "a>b", "a|b", ".git::$INDEX_ALLOCATION", ".git:foo", "git~1::$DATA", "CON:ads")
	add("it ends in a dot or a space, which Windows strips", "v1.", "v1 ", ".git ", ".git.", ".git . . .", "git~1 ", "git~1.", "git~1 . ", "GIT~1.")
	add("Windows keeps it for a device", "CON", "con", "CON.txt", "CON .txt", "Nul", "PRN", "AUX", "COM0", "COM9", "LPT1", "COM\u00b9",
		"lpt\u00b3", "CONIN$", "conout$.log", "a/nul.tar.gz")
	add("it is an 8.3 short name, which NTFS may give another name", "GIT~1", "git~1", "git~2", "git~10", "PROGRA~1.TXT", "gi7eba~1",
		"gitmod~1", "GITMOD~4", "a~12", "~1")
	add("a control character", "a\x00b", "a\x1fb", "a\x7fb", "a\u0085b", "a\tb")
	add("a format character, which macOS may strip", "\u200e.git", ".g\u200ci\u200dt", ".gitmod\u200dules", ".gitmodules\ufeff", "a\u00adb",
		"a\u2060b", "a\u200bb")
	// The code points HFS+ strips from a name, after git's utf8.c next_hfs_char, each in .git.
	for _, r := range []rune{0x200c, 0x200d, 0x200e, 0x200f, 0x202a, 0x202b, 0x202c, 0x202d, 0x202e, 0x206a, 0x206b, 0x206c, 0x206d, 0x206e, 0x206f, 0xfeff} {
		add("a format character, which macOS may strip", fmt.Sprintf(".g%cit", r), fmt.Sprintf("%c.git", r), fmt.Sprintf(".git%c", r))
	}
	add("a default-ignorable character, which shows as nothing", "a\u034fb", ".g\u034fit", "a\ufe00b", "a\ufe0fb", "a\U000e0100b",
		"a\u180bb", "a\u180fb", "a\u115fb", "a\u1160b", "a\u17b4b", "a\u3164b", "a\uffa0b", "a\ufff0b", "a\U000e01f0b", ".git\ufe0f",
		".gi\u034ft", "a\u1160", "a\u3164")
	add("a character that folds to several", "\ufb01le", "\u0130", "\u0149", "\u01f0", "\u0390", "\u0587", "\u1e96", "\u1f80", "\u1fb3",
		"\u1fe4", "\u1ffc", "\u1f56")
	add("its composed form", "\u03b1\u0345", "\u03b9\u0308\u0301", "I\u0307") // ᾳ, ΐ and İ decomposed
	add("its decomposed form", "a\u226e", "\u226f")                           // ≮ and ≯, whose < and > Windows reserves
	add("it is .git, in some case", ".git", ".Git", ".GIT", ".gIt", ".g\u0131t", ".git/config")
	add("names a file's fork", "f/..namedfork/rsrc", "..namedfork", "f/..NamedFork/data")
	return list
}

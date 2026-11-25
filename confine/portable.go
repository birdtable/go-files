package confine

import (
	"fmt"
	"io/fs"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// The rules for .git, its 8.3 short name and the HFS+ ignorable code points are ported from go-git
// v5.19.3's internal/pathutil (dotgit.go, hfs.go, ntfs.go), git's own verify_path, Copyright 2018
// Sourced Technologies, S.L., under the Apache License, Version 2.0, as this module is; NOTICE
// names it. The character set, the trailing dot and space, the length and the key are this
// package's.

// maxName is the longest name, in bytes, that every system takes: ext4 and the BSDs keep 255
// bytes, and NTFS and APFS 255 characters.
const maxName = 255

// Portable answers whether name, one name in a folder, means one file, and the same one, on every
// system this module builds for, by the strictest of their rules on every host, and its key: the
// form two names share when a folder that folds case reads them as one. It looks at nothing on
// disk. A path is checked a name at a time; "/" is refused in a name.
//
// It refuses, with an error matching fs.ErrInvalid:
//   - an empty name, ".", "..", a name that is not UTF-8, and one longer than 255 bytes;
//   - a control character, and any of / \ : * ? " < > |, which Windows reserves;
//   - a name that ends in a dot or a space, which Windows strips, so v1. would be v1;
//   - a name Windows keeps for a device, as CON, NUL, COM1, LPT¹ or CONIN$, in any case and with
//     any extension, as CON.txt;
//   - an 8.3 short name, a ~ and a number ending the part before the first dot, as GIT~1 or
//     PROGRA~1.TXT, which NTFS may give another name in the folder;
//   - a format character, the HFS+ ignorable code points among them, which macOS strips, so a name
//     with one in .git is .git there;
//   - a default-ignorable character that is not a format character, as the combining grapheme
//     joiner U+034F, a variation selector, a Hangul filler or a Mongolian free variation selector,
//     by Unicode's Other_Default_Ignorable_Code_Point and Variation_Selector: each shows as nothing
//     and no normal form removes it, so a name with one reads as the name without it;
//   - a character whose full case folding is several, as İ, the ligature ﬁ or the Greek Extended
//     letters with a ypogegrammeni, as ᾳ, by Unicode's CaseFolding.txt, but for ß and ẞ, whose key
//     is ss;
//   - .git in any case, the folder git keeps a repository in;
//   - a name whose composed (NFC) or decomposed (NFD) form, by Unicode's canonical equivalence, a
//     rule above but the length refuses: macOS keeps a name decomposed, so ≮ is < and a slash
//     there, and the < is Windows'.
//
// A name in any normal form passes: macOS takes a name in any of them, so é composed and e with
// a combining acute are one name there, and the key settles them. The key is the name decomposed
// (NFD), each character folded to one of its case, its upper case and then the first of that
// case's simple folding orbit in lower case, and composed again (NFC), by golang.org/x/text's
// unicode/norm. So S, s and the long s ſ share one key, ß and ẞ are ss, é and e with a combining
// acute share one, the Kelvin sign is k, and a CJK compatibility ideograph's key is the unified
// one. Two names Portable passes are one name to a folder that folds case or a canonical normal
// form, on any system, only when their keys are equal. Left open: a folder that folds the
// compatibility forms, as a ZFS dataset made with normalization=formKC or formKD, reads a
// fullwidth ｇ as g, so .ｇit passes with its own key and is .git there, and a and ａ are two keys
// for one name.
func Portable(name string) (key string, err error) {
	if why := unportable(name); why != "" {
		return "", fmt.Errorf("%q is not a portable name: %s: %w", name, why, fs.ErrInvalid)
	}
	key = portableKey(name)
	if key == ".git" {
		return "", fmt.Errorf("%q is not a portable name: it is .git, in some case: %w", name, fs.ErrInvalid)
	}
	return key, nil
}

// unportable is why name is not portable, or "" when each rule but .git's passes it.
func unportable(name string) string {
	switch {
	case !utf8.ValidString(name):
		return "it is not UTF-8"
	case len(name) > maxName:
		return fmt.Sprintf("it is longer than %d bytes", maxName)
	}
	if why := unportableForm(name); why != "" {
		return why
	}
	if composed := norm.NFC.String(name); composed != name {
		if why := unportableForm(composed); why != "" {
			return "its composed form " + strings.TrimPrefix(why, "it ")
		}
	}
	if decomposed := norm.NFD.String(name); decomposed != name {
		if why := unportableForm(decomposed); why != "" {
			return "its decomposed form " + strings.TrimPrefix(why, "it ")
		}
	}
	return ""
}

// unportableForm is why name, valid UTF-8 in the form given, is not portable by a rule but the
// length's, or "".
func unportableForm(name string) string {
	switch {
	case name == "" || name == "." || name == "..":
		return "it is empty, . or .."
	case strings.ContainsAny(name, `/\:*?"<>|`):
		return `it holds one of / \ : * ? " < > |`
	case strings.HasSuffix(name, ".") || strings.HasSuffix(name, " "):
		return "it ends in a dot or a space, which Windows strips"
	case reservedDevice(name):
		return "Windows keeps it for a device"
	case shortName(name):
		return "it is an 8.3 short name, which NTFS may give another name"
	}
	for _, r := range name {
		if why := unportableRune(r); why != "" {
			return fmt.Sprintf("it holds %U, %s", r, why)
		}
	}
	return ""
}

// unportableRune is why r has no place in a portable name, or "".
func unportableRune(r rune) string {
	switch {
	case unicode.IsControl(r):
		return "a control character"
	case unicode.Is(unicode.Cf, r):
		return "a format character, which macOS may strip"
	case unicode.In(r, unicode.Other_Default_Ignorable_Code_Point, unicode.Variation_Selector):
		return "a default-ignorable character, which shows as nothing"
	case r != 'ß' && r != 'ẞ' && multiFold(r):
		return "a character that folds to several"
	}
	return ""
}

// multiFold is whether r's full case folding is more than one character, by Unicode's
// CaseFolding.txt (its F rows): ß, İ, ŉ, ǰ, ΐ, ΰ, և, ẖ to ẚ, ẞ, the Greek Extended letters with
// a breathing and a perispomeni or with a ypogegrammeni, and the Latin and Armenian ligatures.
func multiFold(r rune) bool {
	switch r {
	case 0x00df, 0x0130, 0x0149, 0x01f0, 0x0390, 0x03b0, 0x0587, 0x1e9e,
		0x1f50, 0x1f52, 0x1f54, 0x1f56, 0x1fb6, 0x1fb7, 0x1fbc, 0x1fc6, 0x1fc7, 0x1fcc,
		0x1fd2, 0x1fd3, 0x1fd6, 0x1fd7, 0x1fe2, 0x1fe3, 0x1fe4, 0x1fe6, 0x1fe7, 0x1ff6, 0x1ff7, 0x1ffc:
		return true
	}
	return 0x1e96 <= r && r <= 0x1e9a || 0x1f80 <= r && r <= 0x1faf || 0x1fb2 <= r && r <= 0x1fb4 ||
		0x1fc2 <= r && r <= 0x1fc4 || 0x1ff2 <= r && r <= 0x1ff4 ||
		0xfb00 <= r && r <= 0xfb06 || 0xfb13 <= r && r <= 0xfb17
}

// reservedDevice is whether Windows keeps name for a device: the part before the first dot, its
// trailing spaces cut, is CON, PRN, AUX, NUL, CONIN$, CONOUT$, or COM or LPT and a digit or a
// superscript one, two or three, in any case. A name is held to it on every system.
func reservedDevice(name string) bool {
	base, _, _ := strings.Cut(name, ".")
	base = strings.ToUpper(strings.TrimRight(base, " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	rest, ok := strings.CutPrefix(base, "COM")
	if !ok {
		rest, ok = strings.CutPrefix(base, "LPT")
	}
	return ok && (len(rest) == 1 && '0' <= rest[0] && rest[0] <= '9' || rest == "¹" || rest == "²" || rest == "³")
}

// shortName is whether name has the form of an 8.3 short name NTFS makes for a long one: at most
// eight bytes before the first dot, ending in a ~ and a number that does not start with 0, and at
// most three after it, with no second dot. GIT~1 is .git's.
func shortName(name string) bool {
	base, ext, _ := strings.Cut(name, ".")
	if len(base) > 8 || len(ext) > 3 || strings.Contains(ext, ".") {
		return false
	}
	i := strings.LastIndexByte(base, '~')
	digits := base[i+1:]
	if i < 0 || digits == "" || digits[0] == '0' {
		return false
	}
	return strings.Trim(digits, "0123456789") == ""
}

// portableKey decomposes name (NFD), folds each character to one of its case, its upper case and
// then the first of that case's simple folding orbit in lower case, and ß and ẞ to ss, and
// composes the result (NFC).
func portableKey(name string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(name) {
		if r == 'ß' || r == 'ẞ' {
			b.WriteString("ss")
			continue
		}
		r = unicode.ToUpper(r)
		first := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			first = min(first, f)
		}
		b.WriteRune(unicode.ToLower(first))
	}
	return norm.NFC.String(b.String())
}

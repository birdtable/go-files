package confine_test

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"github.com/birdtable/go-files/confine"
)

// whyMulti is the reason Portable says of a character whose full case folding is several.
const whyMulti = "folds to several"

// Portable passes a plain name and answers its key, one for two names a folder that folds case
// reads as one, and another for two it does not.
//
// What it costs when this fails: a name every system takes is refused, or two names one folder
// reads as one have two keys, and a check for a clash misses it.
func TestPortablePassesAPlainNameWithItsKey(t *testing.T) {
	t.Parallel()
	keys := map[string]string{
		"a":               "a",
		"README.md":       "readme.md",
		"readme.MD":       "readme.md",
		".gitignore":      ".gitignore",
		".gitmodules":     ".gitmodules",
		".GITMODULES":     ".gitmodules",
		".git2":           ".git2",
		".gxt":            ".gxt",
		"git":             "git",
		"gitfoo":          "gitfoo",
		"CONNECT":         "connect",
		"COM":             "com",
		"COM10":           "com10",
		"comic":           "comic",
		"CON2":            "con2",
		"git~0":           "git~0",
		"backup~1.tar.gz": "backup~1.tar.gz",
		"a~b":             "a~b",
		"toolongname~1":   "toolongname~1",
		"caf\u00e9":       "caf\u00e9",
		"CAF\u00c9":       "caf\u00e9",
		"Stra\u00dfe":     "strasse",
		"STRASSE":         "strasse",
		"STRA\u1e9eE":     "strasse",
		"\u017f":          "s",
		"\u0131":          "i",
		"\u03a3":          "\u03c3",
		"\u03c2":          "\u03c3",
		"\u03c3":          "\u03c3",
		"\ud55c":          "\ud55c",
		// A name in any normal form passes, and its key settles the spellings.
		"cafe\u0301":             "caf\u00e9",
		"CAFE\u0301":             "caf\u00e9",
		"\u1100\u1161":           "\uac00",
		"\uac00":                 "\uac00",
		"a\u0903":                "a\u0903",
		"\u0344":                 "\u0308\u0301",
		"\u1f71":                 "\u03ac",
		"\u03ac":                 "\u03ac",
		"\u1f00":                 "\u1f00",
		"\u1f08":                 "\u1f00",
		"\u212a":                 "k",
		"\u2126":                 "\u03c9",
		"\u212b":                 "\u00e5",
		"\u0374":                 "\u02b9",
		"\u037e":                 ";",
		"\u0387":                 "\u00b7",
		"\u2000":                 "\u2002",
		"\u2001":                 "\u2003",
		"\u2329":                 "\u3008",
		"\u232a":                 "\u3009",
		"\uf900":                 "\u8c48",
		"\U0002f800":             "\u4e3d",
		"\u0958":                 "\u0915\u093c",
		"\u0915\u093c":           "\u0915\u093c",
		" lead":                  " lead",
		strings.Repeat("a", 255): strings.Repeat("a", 255),
	}
	for name, want := range keys {
		if got, err := confine.Portable(name); err != nil || got != want {
			t.Errorf("Portable(%+q) = %q, %v, want %q", name, got, err, want)
		}
	}
	a, _ := confine.Portable("Makefile")
	b, _ := confine.Portable("makefile")
	c, _ := confine.Portable("makefile2")
	if a != b || a == c {
		t.Errorf("the keys of Makefile, makefile and makefile2 are %q, %q and %q, want the first two one and the third another", a, b, c)
	}
}

// Every character with a canonical decomposition, after an a, is one name with its decomposed
// spelling: both pass with one key, or both are refused.
//
// What it costs when this fails: é composed and e with a combining acute, one name to macOS, have
// two keys, and a check for a clash misses it.
func TestEachSpellingOfANameHasOneKey(t *testing.T) {
	t.Parallel()
	held := 0
	for r := rune(0); r <= 0x2ffff; r++ {
		composed := string(r)
		decomposed := norm.NFD.String(composed)
		if !utf8.ValidRune(r) || decomposed == composed {
			continue
		}
		held++
		k1, e1 := confine.Portable("a" + composed)
		k2, e2 := confine.Portable("a" + decomposed)
		if (e1 == nil) != (e2 == nil) || k1 != k2 {
			t.Errorf("Portable(a%+q) = %q, %v and Portable(a%+q) = %q, %v, want one key or two refusals", composed, k1, e1, decomposed, k2, e2)
		}
	}
	if held < 2000 {
		t.Fatalf("held %d characters with a decomposition, and want Unicode's two thousand or more", held)
	}
}

// Portable refuses, as folding to several, a letter exactly when its full case folding is more
// than one character, by golang.org/x/text's cases.Fold, but for ß and ẞ, whose key is ss.
//
// What it costs when this fails: a letter macOS may fold to two characters passes with a key of
// one, and the two spellings of a name have two keys.
func TestPortableRefusesALetterExactlyWhenItsFullFoldIsSeveral(t *testing.T) {
	t.Parallel()
	fold := cases.Fold()
	refused := 0
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if !unicode.IsLetter(r) {
			continue
		}
		several := utf8.RuneCountInString(fold.String(string(r))) > 1 && r != 'ß' && r != 'ẞ'
		_, err := confine.Portable("a" + string(r))
		if says := err != nil && strings.Contains(err.Error(), whyMulti); says != several {
			t.Errorf("Portable(a%+q) = %v, and want a refusal for folding to several %v", string(r), err, several)
		}
		if several {
			refused++
		}
	}
	if refused < 100 {
		t.Fatalf("refused %d letters, and want CaseFolding.txt's hundred or more", refused)
	}
}

// Portable refuses every default-ignorable character, Unicode's Default_Ignorable_Code_Point,
// which Linux's casefold drops from a name: each format character, each of
// Other_Default_Ignorable_Code_Point, the Hangul fillers among them, and each variation selector.
// It refuses a little more than the derived property, as the format characters that show, which
// macOS may strip.
//
// What it costs when this fails: a name with one passes with a key its own, and an ext4, f2fs or
// tmpfs folder with casefold reads it as the name without it, .git among them.
func TestPortableRefusesEveryDefaultIgnorable(t *testing.T) {
	t.Parallel()
	judged := 0
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if !unicode.In(r, unicode.Cf, unicode.Other_Default_Ignorable_Code_Point, unicode.Variation_Selector) {
			continue
		}
		name := "a" + string(r) + "b"
		if key, err := confine.Portable(name); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Portable(%+q) = %q, %v, want a refusal matching fs.ErrInvalid", name, key, err)
		}
		judged++
	}
	if judged < 4200 {
		t.Fatalf("judged %d code points, and want Unicode 17's 4206", judged)
	}
}

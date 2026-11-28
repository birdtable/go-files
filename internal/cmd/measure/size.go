package main

import (
	"bytes"
	"io/fs"
	"strings"
)

// Ceiling is the most non-test Go lines the module's packages but Apart may hold: the count at v0.3.0,
// 5,875, moved by each later change by exactly the lines it was measured to add or remove, and by
// nothing else, with no headroom. A change that adds or removes lines moves it in the same commit,
// which says what the lines are.
const Ceiling = 15617

// Apart is the package counted on its own, under ApartCeiling, and left out of Ceiling's count.
const (
	Apart        = "treesum"
	ApartCeiling = 548 // the count at v0.2.0, 462, moved as Ceiling is: by the measured lines of each change, and nothing else
)

// Counted is the one folder of internal/ the count reads: the Windows calls write, confine and
// lock and appdirs share, which is library code a program links.
const Counted = "internal/winapi"

// Size counts the non-test Go lines of the module's packages the way `wc -l` of their files
// does, one per newline: every .go file not named _test.go, outside internal/ but Counted,
// testdata and any folder whose name starts with a dot. It gives the count of Apart's folder apart from the
// rest's.
func Size(fsys fs.FS) (rest, apart int, err error) {
	err = fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipped(name) {
				return fs.SkipDir
			}
			return nil
		}
		if !source(name) || strings.HasPrefix(name, "internal/") && !strings.HasPrefix(name, Counted+"/") {
			return nil // a file of internal/ itself is not Counted's either
		}
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		if strings.HasPrefix(name, Apart+"/") {
			apart += bytes.Count(b, []byte("\n"))
		} else {
			rest += bytes.Count(b, []byte("\n"))
		}
		return nil
	})
	return rest, apart, err
}

// skipped says whether the folder name, a path from the module's root, holds no package of the
// module's surface: a folder of internal/ but Counted, testdata, and a folder whose name starts
// with a dot.
func skipped(name string) bool {
	if name == "." {
		return false
	}
	base := name[strings.LastIndex(name, "/")+1:]
	internal := strings.HasPrefix(name, "internal/") && name != Counted && !strings.HasPrefix(name, Counted+"/")
	return internal || base == "testdata" || strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_")
}

// source says whether the file name is a Go file a build of the package reads.
func source(name string) bool {
	base := name[strings.LastIndex(name, "/")+1:]
	return strings.HasSuffix(base, ".go") && !strings.HasSuffix(base, "_test.go") &&
		!strings.HasPrefix(base, ".") && !strings.HasPrefix(base, "_")
}

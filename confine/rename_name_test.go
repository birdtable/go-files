package confine

import "testing"

// trailed finds a component of a name, the last or one before it, that ends in a dot or a space,
// and nothing in a name with none. It runs on every system, so the check RenameNoReplace makes on
// Windows is seen here too.
//
// What it costs when this fails: on Windows x. is x to os.Root and x. to the move, so a look sees
// one file and the move makes another, which no Win32 program opens by its name.
func TestAComponentThatEndsInADotOrASpaceIsFound(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{
		"x.":          true,
		"x ":          true,
		"x. ":         true,
		"x .":         true,
		"sub./x":      true,
		"sub /x":      true,
		"sub/x.":      true,
		"x":           false,
		"x.txt":       false,
		".hidden":     false,
		" x":          false,
		"sub/x.txt":   false,
		"sub.d/x":     false,
		"sub/.git/x":  false,
		"a b/c d.txt": false,
	} {
		if got := trailed(name, "/"); got != want {
			t.Errorf("trailed(%q) = %v, want %v", name, got, want)
		}
	}
}

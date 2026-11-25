package filestest_test

import (
	"strings"
	"testing"

	"github.com/birdtable/go-files/filestest"
)

// AllPortable says nothing of paths every system takes as distinct files, a path given twice and
// a folder shared by several among them.
//
// What it costs when this fails: a product's test of its own names fails on names that are fine,
// and the check is turned off.
func TestAllPortableSaysNothingOfPortablePaths(t *testing.T) {
	t.Parallel()
	r := (&recorder{TB: t}).run(func(r *recorder) {
		filestest.AllPortable(r, "README.md", "docs/guide.md", "docs/guide.md", "docs/Guide2.md", "caf\u00e9/a", "caf\u00e9/b", "a/b/c", "a/b/d")
		filestest.AllPortable(r)
	})
	if r.failed {
		t.Errorf("AllPortable of portable paths: %q, and want nothing said", r.said)
	}
}

// AllPortable fails the test once, naming each path refused with its rule and each pair that
// meets where a folder folds case or a normal form, as files and as folders, in sorted order.
//
// What it costs when this fails: a product ships two names that are one file on macOS or Windows,
// or a name Windows keeps for a device, and its test of its names said so for the first alone.
func TestAllPortableNamesEveryRefusalAndEveryPairOnce(t *testing.T) {
	t.Parallel()
	r := (&recorder{TB: t}).run(func(r *recorder) {
		filestest.AllPortable(r, "a/README", "a/readme", "caf\u00e9", "cafe\u0301", "Docs/x", "docs/y", "src/CON.txt", "src/ok", "C:/x")
	})
	if !r.failed || len(r.said) != 1 {
		t.Fatalf("AllPortable: failed %v, said %q, and want one failure", r.failed, r.said)
	}
	for _, want := range []string{
		`"src/CON.txt": "CON.txt" is not a portable name: Windows keeps it for a device`,
		`"C:/x": "C:" is not a portable name: it holds one of`,
		`"a/README" and "a/readme" meet`,
		`"cafe\u0301" and "caf\u00e9" meet`, // e, 0x65, sorts before the composed é's 0xc3
		`"Docs/x" and "docs/y" meet where a folder folds case or a normal form: "Docs" and "docs" share the key "docs"`,
	} {
		if !r.says(want) {
			t.Errorf("AllPortable said %q, and want it to say %q", r.said[0], want)
		}
	}
	if r.says(`"src/ok"`) {
		t.Errorf("AllPortable said %q, and want nothing of src/ok", r.said[0])
	}
	lines := strings.Split(r.said[0], "\n\t")[1:]
	for i := 1; i < len(lines); i++ {
		if lines[i-1] > lines[i] {
			t.Errorf("AllPortable's lines are out of order: %q before %q", lines[i-1], lines[i])
		}
	}
	if len(lines) != 5 {
		t.Errorf("AllPortable said %d lines, %q, and want 5", len(lines), lines)
	}
}

// AllPortable fails the test for each of the hostile names alone, with the name's rule.
//
// What it costs when this fails: a product's test of its names passes a name the library's list
// says a program must refuse.
func TestAllPortableFailsEachHostileName(t *testing.T) {
	t.Parallel()
	for _, h := range filestest.HostileNames() {
		r := (&recorder{TB: t}).run(func(r *recorder) { filestest.AllPortable(r, h.Path) })
		if !r.failed || !r.says(h.Why) {
			t.Errorf("AllPortable(%+q): failed %v, said %q, and want a failure that says %q", h.Path, r.failed, r.said, h.Why)
		}
	}
}

// Each call of HostileNames answers a list of its own, so a test that changes what it was given
// leaves the next test's list as it was.
//
// What it costs when this fails: one test's change to the list reaches another's, and a hostile
// name goes unchecked there.
func TestHostileNamesAnswersAFreshListEachCall(t *testing.T) {
	t.Parallel()
	a := filestest.HostileNames()
	want := a[0]
	a[0] = filestest.HostileName{Path: "fine", Why: "changed"}
	if b := filestest.HostileNames(); b[0] != want || len(b) != len(a) {
		t.Errorf("HostileNames after a change to an earlier answer: %+v, and want %+v first", b[0], want)
	}
}

package confine

import (
	"os"
	"testing"
)

// plantLink makes a symbolic link at link to target. On Windows the test session makes one
// without elevation, as developer mode is on there, so a refusal fails the test like any other.
func plantLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

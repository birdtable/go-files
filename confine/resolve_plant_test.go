package confine_test

import (
	"testing"

	"github.com/birdtable/go-files/confine"
)

// plantLink makes link a symbolic link to target, as written.
func plantLink(t *testing.T, target, link string) {
	t.Helper()
	confine.PlantLink(t, target, link)
}

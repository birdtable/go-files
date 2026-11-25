//go:build (unix || windows) && !linux

package appdirs

import "testing"

// overflowTaken is whether trust takes Who.Overflow on this system at all: Linux alone.
const overflowTaken = false

// uidMapMapsRoot is never asked off Linux, which has no uid_map.
func uidMapMapsRoot(t *testing.T) bool {
	t.Helper()
	t.Fatal("no uid_map off Linux")
	return false
}

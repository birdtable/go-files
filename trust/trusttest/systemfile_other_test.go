//go:build !linux && !windows

package trusttest_test

import "testing"

// wantSystemOwner is root, which owns each candidate on macOS and another Unix.
func wantSystemOwner(*testing.T) string { return "uid:0" }

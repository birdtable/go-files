//go:build !darwin && !linux && !windows

package confine_test

import "testing"

// foldsFolders is no folder beyond the temp folder on a system Folds does not ask.
func foldsFolders(*testing.T) []string { return nil }

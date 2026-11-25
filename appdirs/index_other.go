//go:build !windows

package appdirs

import "os"

// indexAtPath refuses nothing off Windows: every Unix volume gives a folder an inode os.SameFile can
// trust.
func indexAtPath(string, os.FileInfo, string, error) error { return nil }

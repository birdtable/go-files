//go:build !windows

package treesum

import "os"

// indexAt refuses nothing off Windows: every Unix volume gives a folder an inode os.SameFile can trust.
func indexAt(*os.Root, string) error { return nil }

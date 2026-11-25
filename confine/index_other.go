//go:build !windows

package confine

import "os"

// indexHeld and indexAtPath refuse nothing off Windows: every Unix volume gives a file an inode
// os.SameFile can trust, so a compare by file ID is never on a volume with none.
func indexHeld(*os.File, string) error { return nil }

func indexSame(*os.Root, string, os.FileInfo) error { return nil }

func indexRoot(*os.Root, os.FileInfo, string, error) error { return nil }

func indexAtPath(string, os.FileInfo, string, error) error { return nil }

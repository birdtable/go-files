//go:build !windows

package confine

import "os"

// rootOnPipes refuses nothing: only Windows has a pipes' device a root can be opened on.
func rootOnPipes(*os.Root, string) error { return nil }

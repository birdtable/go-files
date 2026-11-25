//go:build unix && !linux

package trust

import "os"

// aclMask is false off Linux: no other system's access list shows in the mode's group bits, so
// the walk reads those bits as they are. macOS's list is read by listOf and judged by judgeMac,
// and every other Unix's is refused by listOf.
func aclMask(string) (bool, error) { return false, nil }

// aclMaskFile is aclMask of a file held, false off Linux as it is.
func aclMaskFile(*os.File) (bool, error) { return false, nil }

const overflowUID = "" // off Linux, no user namespace shows an overflow account

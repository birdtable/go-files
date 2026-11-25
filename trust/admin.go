package trust

import (
	"fmt"
	"strconv"
)

// AdminGroup is the system's admin group: on macOS the group "admin" names, read by os/user at
// each call, and none elsewhere, ok false. A failed lookup, or a gid that is not a decimal 32-bit
// number, is an error, never a guess. It trusts nothing: a program that lets it in says so in Who.Group.
func AdminGroup() (gid uint32, ok bool, err error) { return adminGroup() }

// adminOf is the gid lookup gives for name, held on every system to a decimal 32-bit number.
func adminOf(name string, lookup func(string) (string, error)) (uint32, bool, error) {
	s, err := lookup(name)
	if err != nil {
		return 0, false, fmt.Errorf("trust: the admin group %q cannot be read: %w", name, err)
	}
	gid, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, false, fmt.Errorf("trust: the admin group %q has the gid %q, not a decimal 32-bit number", name, s)
	}
	return uint32(gid), true, nil
}

//go:build !darwin

package trust

// adminGroup is none off macOS: sudo, wheel and Administrators are a site's or not a gid.
func adminGroup() (uint32, bool, error) { return 0, false, nil }

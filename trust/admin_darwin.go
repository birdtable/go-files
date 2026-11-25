package trust

import "os/user"

// adminGroup is the group macOS names "admin", which os/user reads with cgo or without.
func adminGroup() (uint32, bool, error) {
	return adminOf("admin", func(name string) (string, error) {
		g, err := user.LookupGroup(name)
		if err != nil {
			return "", err
		}
		return g.Gid, nil
	})
}

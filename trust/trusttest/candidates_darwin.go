package trusttest

// candidates is a file of /private/etc, named with no link on the way as /etc is not, and a
// program System Integrity Protection keeps.
func candidates() ([]string, error) { return []string{"/private/etc/hosts", "/usr/bin/true"}, nil }

//go:build !windows && !darwin

package trusttest

// candidates is the account file and the shell every Linux and other Unix keeps.
func candidates() ([]string, error) { return []string{"/etc/passwd", "/bin/sh"}, nil }

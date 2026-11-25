//go:build !windows

package testtoken

// Plain does nothing off Windows, and answers nil: no other system gives a process a privilege
// that Plain disables.
func Plain() error { return nil }

//go:build !linux

package trust

import "os"

// readProc reads path as any file: only Linux's /proc is read, for an overflow account, which
// other systems have not.
func readProc(path string) ([]byte, error) { return os.ReadFile(path) } //nolint:gosec // G304: a fixed name, never read off Linux

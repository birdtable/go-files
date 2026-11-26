package appdirs

// ReparseTag is reparseTag, the reparse tag of a name on Windows, no link followed, for the tests
// of package appdirs_test. It is built into the test binary only.
func ReparseTag(path string) (uint32, error) { return reparseTag(path) }

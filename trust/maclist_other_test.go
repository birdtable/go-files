//go:build unix && !darwin

package trust

// listByName is false off macOS: ReadOwned reads no access list by a file's name.
const listByName = false

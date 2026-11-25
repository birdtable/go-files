//go:build !unix

package nowait

// noWait is nothing: on Windows an open of a name never waits for a pipe's other end, and on the
// other systems that are not Unix no flag is added.
const noWait = 0

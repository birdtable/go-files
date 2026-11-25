//go:build !linux && !darwin

package confine

// kernelOpens is false: open is the walk, which opens the last component in the folder it holds.
const kernelOpens = false

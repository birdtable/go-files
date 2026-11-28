// Package imagetest takes off a disk image a test attached on a Mac, the one way trust's and
// confine's tests share so the two cannot drift (go-files#55). It is the tests' alone, and no
// package's code imports it. It imports the standard library and golang.org/x/sys.
package imagetest

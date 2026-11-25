// Command measure prints the size of the module's code and finds the files no lint run reads. It
// imports the standard library alone, and no package imports it. The Taskfile runs it from the
// module's root:
//
//	measure unread  fails on a file the go command can build and golangci-lint does not read
//	measure size    prints the non-test Go lines, treesum's apart, and fails above either ceiling
//
// The exported surface it once wrote to API.txt is held by apidiff in task api.
package main

import "os"

func main() { os.Exit(run(os.Args[1:], ".", os.Stdout)) }

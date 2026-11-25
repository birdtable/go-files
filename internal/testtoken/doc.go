// Package testtoken gives a test process the token a program's process has: Plain makes it so, and
// First is Plain as the step a package's TestMain gives testkit.Main before any test, which Held
// checks ran. It is the tests' alone, and no package's code imports it.
// It imports the standard library and golang.org/x/sys.
package testtoken

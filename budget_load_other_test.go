//go:build !windows

package gofiles_test

// handedLoadLine is what hatch budget says of a tier that ends within its budget with a load read at
// its start: the load before, which only budget's --load-before can give it, beside
// the load hatch reads at the end. budget_load_windows_test.go has Windows' line.
const handedLoadLine = `^budget: load 16\.2 before pre-push, `

// Command accounthome is a program built as programs are, not a test binary, that prints
// appdirs.AccountHome's answer as three lines: home=, refused= (whether the error matches
// ErrNoAccountHome) and err=. appdirs' tests build it with the osusergo tag and without cgo, where
// os/user falls back to HOME, and run it with HOME set to a folder of the test's.
package main

import (
	"errors"
	"fmt"

	"github.com/birdtable/go-files/appdirs"
)

func main() {
	home, err := appdirs.AccountHome()
	fmt.Printf("home=%s\nrefused=%t\nerr=%v\n", home, errors.Is(err, appdirs.ErrNoAccountHome), err)
}

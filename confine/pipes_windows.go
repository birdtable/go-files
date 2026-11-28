package confine

import (
	"os"

	"github.com/birdtable/go-files/internal/winapi"
)

// rootOnPipes refuses root, before any open beneath it, where its folder is a host's pipes, as
// winapi.RootOnPipes reads it. Each confine call that opens a name beneath a caller's root asks it
// first.
func rootOnPipes(root *os.Root, name string) error { return winapi.RootOnPipes(root, name) }

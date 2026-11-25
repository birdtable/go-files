package appdirs_test

import (
	"fmt"
	"log"
	"os"

	"github.com/birdtable/go-files/appdirs"
	"github.com/birdtable/go-files/confine"
)

// cacheIn makes the folder cache beneath the program app's own temp folder and answers it as a
// root: the temp folder opened as a root, and the sub-folder made beneath it by confine.Mkdir,
// which follows no link, refuses a folder swapped on the way and answers the folder it made, so
// there is no join-then-mkdir race. appdirs has no call of its own for this, and
// appdirs/README.md shows this function as it is.
func cacheIn(app string) (*os.Root, error) {
	dir, err := appdirs.Temp(app)
	if err != nil {
		return nil, err
	}
	top, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer top.Close()
	return confine.Mkdir(top, "cache", 0o700) // on Windows the owner-only list, as Temp's
}

// A program keeps a cache beneath its own temp folder, made by handle. appdirs/README.md shows
// cacheIn as it is.
func ExampleTemp_sub() {
	cache, err := cacheIn("go-files-example")
	if err != nil {
		log.Fatal(err)
	}
	defer cache.Close()
	if err := cache.WriteFile("index", []byte("v1"), 0o600); err != nil {
		log.Fatal(err)
	}
	b, err := cache.ReadFile("index")
	fmt.Println(string(b), err)
	// Output: v1 <nil>
}

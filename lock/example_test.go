package lock_test

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/birdtable/go-files/lock"
)

// One run of a program changes the machine at a time: while the first holds the lock, a second
// is declined and told who holds it. The lock is in the program's own folder, made 0700 under a
// name no other account chose, so no other account can take the lock first or put a link at its
// name. The README shows this function as it is, and a test holds the two the same.
func ExampleTake() {
	dir, err := os.MkdirTemp("", "myprogram")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "myprogram.lock")
	note := filepath.Join(dir, "myprogram.holder") // the program names its note: lock names none

	l, err := lock.Take(path, note)
	if err != nil {
		log.Fatal(err)
	}
	defer l.Release()
	if err := l.Note("building"); err != nil {
		log.Fatal(err)
	}

	_, err = lock.Take(path, note) // a second run, while the first holds the lock
	var h lock.Held
	fmt.Println(errors.As(err, &h) && h.By == os.Getpid())
	// Output: true
}

// sweep removes every run-* folder beneath base, the folders runs leave behind, while it holds
// the lock at path: the one lock each run holds for as long as it uses its folder, kept beside
// base, never in it. It holds the lock until the last removal, so no run starts in between, and
// removes through an os.Root at base, so a link is removed, never followed. A run that holds the
// lock ends the sweep with an error lock.IsHeld reports, and nothing is removed. removed is told
// each name once it is gone. lock/README.md shows this function as it is.
func sweep(path, base string, removed func(name string)) (err error) {
	l, err := lock.Take(path, "")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, l.Release()) }()
	root, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer root.Close()
	names, err := fs.Glob(root.FS(), "run-*")
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := root.RemoveAll(name); err != nil {
			return err
		}
		removed(name)
	}
	return nil
}

// Two runs stopped and left their folders; a sweep removes them. lock/README.md shows this
// function as it is.
func ExampleTake_sweep() {
	dir, err := os.MkdirTemp("", "myprogram")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	base := filepath.Join(dir, "scratch")
	if err := os.Mkdir(base, 0o700); err != nil {
		log.Fatal(err)
	}
	for range 2 {
		if _, err := os.MkdirTemp(base, "run-"); err != nil {
			log.Fatal(err)
		}
	}

	n := 0
	err = sweep(filepath.Join(dir, "scratch.lock"), base, func(string) { n++ })
	fmt.Println(n, err)
	// Output: 2 <nil>
}

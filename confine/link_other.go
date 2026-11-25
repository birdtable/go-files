//go:build !linux && !darwin && !freebsd && !windows

package confine

import (
	"errors"
	"fmt"
	"os"
)

// link refuses, and makes nothing, on NetBSD and the systems the module does not lint by name: a
// link made by name would follow a link swapped into at's folder.
func link(_ *os.Root, _, at string) error {
	return madeErr(at, fmt.Errorf("this system has no call that makes a link from a folder held open, and nothing was made: %w", errors.ErrUnsupported))
}

// absoluteLinks: CopyTree's link text is never written here, as no link is made.
const absoluteLinks = false

// fits takes every target: no link is made here.
func fits(string) error { return nil }

// linkFile refuses, and makes nothing, as link does.
func linkFile(_ *os.Root, _, at string) error {
	return madeErr(at, fmt.Errorf("this system has no call that makes a link from a folder held open: %w", ErrNoFileLink))
}

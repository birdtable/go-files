//go:build !linux && !darwin && !windows

package confine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// errNoFinal is Final's refusal on a system with no call that names an open file.
var errNoFinal = fmt.Errorf("this system has no call that names an open file: %w", errors.ErrUnsupported)

// openFinal refuses with errNoFinal, opening nothing.
func openFinal(string) (*os.File, error) { return nil, errNoFinal }

func finalOf(*os.File) (string, error) { return "", errNoFinal }

func finalHeld(fs.FileInfo) error { return nil }

func finalVolume(*os.File) error { return nil }

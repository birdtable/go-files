package trusttest

import "fmt"

// giveGroup changes nothing: Windows keeps no group id on a name for chown to change.
func giveGroup(_ string, _ uint32, _ func(string, int, int) error) (func() error, error) {
	return nil, fmt.Errorf("%w: Windows keeps no group id on a name", errCannot)
}

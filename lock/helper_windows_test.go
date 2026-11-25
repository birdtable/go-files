package lock

import "errors"

// umaskTake is refused: Windows has no umask.
func umaskTake(string, string) error { return errors.New("Windows has no umask") }

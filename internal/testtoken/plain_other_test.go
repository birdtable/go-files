//go:build !windows

package testtoken

import "testing"

// What it costs when this fails: off Windows, where no token is changed, every package's TestMain
// ends its run red before a test has run.
func TestPlainAnswersNilOffWindowsAndAgain(t *testing.T) {
	t.Parallel()
	for i := range 2 {
		if err := Plain(); err != nil {
			t.Fatalf("Plain, call %d: %v, and want nil", i+1, err)
		}
	}
}

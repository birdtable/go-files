package trust

import (
	"path/filepath"
	"testing"
)

// What it costs when this fails: a caller on Windows walks a program Windows never starts, read
// from a #! line Windows does not read.
func TestInterpretersOnWindowsIsNoneForAScript(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	plant(t, d, "s", "#!/bin/sh\n", 0o755)
	if got, err := Interpreters(filepath.Join(d, "s")); got != nil || err != nil {
		t.Errorf("%q, %v, and want none", got, err)
	}
}

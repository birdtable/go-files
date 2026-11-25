//go:build !darwin

package trust

import "testing"

// What it costs when this fails: on Linux, Windows or a BSD a program is handed a group it takes
// for the system's admin group, sudo's, wheel's or a site's own, and lets it write what it runs.
func TestAdminGroupIsNoneOffMacOS(t *testing.T) {
	t.Parallel()
	if gid, ok, err := AdminGroup(); gid != 0 || ok || err != nil {
		t.Errorf("AdminGroup: %d, %t, %v, and want 0, false, nil", gid, ok, err)
	}
}

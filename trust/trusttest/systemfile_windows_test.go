package trusttest_test

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/birdtable/go-files/trust/trusttest"
)

// wantSystemOwner is TrustedInstaller, NT SERVICE\TrustedInstaller, which owns ntdll.dll.
func wantSystemOwner(*testing.T) string {
	return "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"
}

// What it costs when this fails: on Windows a test is handed the file of a folder the environment
// names, which any process sets for the program it starts, so a file of the test's own passes as
// the system's.
func TestSystemFileOnWindowsTakesItsFolderFromTheSystemNotTheEnvironment(t *testing.T) { //nolint:paralleltest // serial: sets SystemRoot and windir, the process environment
	dir := t.TempDir()
	for _, v := range []string{"SystemRoot", "windir"} {
		t.Setenv(v, dir)
	}
	sys, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	path, owner := trusttest.SystemFile(t)
	if !strings.EqualFold(path, filepath.Join(sys, "ntdll.dll")) || strings.HasPrefix(strings.ToLower(path), strings.ToLower(dir)) {
		t.Errorf("SystemFile with SystemRoot and windir at %s: %s, and want ntdll.dll in %s", dir, path, sys)
	}
	systemFileHolds(t, path, owner, wantSystemOwner(t))
}

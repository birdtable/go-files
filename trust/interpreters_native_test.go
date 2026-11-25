//go:build linux || darwin

package trust

import (
	"errors"
	"maps"
	"os/exec" //nolint:depguard // cli.process.1: the test starts each script as the system starts it, to hold Interpreters to the system; the module has no runner
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// What it costs when this fails: Interpreters answers a chain where this system starts none, or
// none where it starts one, so a caller walks programs the system does not run, or refuses a
// script it runs. Each script is started by execve, with no shell's fallback.
func TestInterpretersAgreesWithTheSystemStartingEachScript(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	if strings.ContainsAny(d, " \t#\n\x00") {
		t.Fatalf("the test folder %q holds a byte that ends a #! line, so no script here can name an interpreter in it: set TMPDIR to a folder without one", d)
	}
	slashes := func(n int) string { return strings.Repeat("/", n-len("bin/sh")) + "bin/sh" }
	scripts := map[string]string{
		"plain":       "#!/bin/sh\nexit 0\n",
		"tab":         "#!\t/bin/sh\t-e\n",
		"no newline":  "#!/bin/sh",
		"blank":       "#!   \n",
		"nul":         "#!/bin/sh\x00junk\n",
		"cr":          "#!/bin/sh\r\n",
		"hash":        "#!/bin/sh#x\n",
		"hash before": "#! #/bin/sh\n",
		"relative":    "#!sh\n",
		"folder":      "#!" + d + "\n",
		"pipe for it": "#!" + filepath.Join(d, "pipe") + "\n",
		"not there":   "#!" + filepath.Join(d, "none") + "\n",
		"n0":          "#!/bin/sh\n",
	}
	for i := 1; i <= 5; i++ {
		scripts["n"+strconv.Itoa(i)] = "#!" + filepath.Join(d, "n"+strconv.Itoa(i-1)) + "\n"
	}
	for _, n := range []int{253, 254, 255, 509, 510, 511} {
		scripts["name "+strconv.Itoa(n)] = "#!" + slashes(n) + "\n"
	}
	scripts["space 256th"] = "#!" + slashes(253) + " " + strings.Repeat("e", 50) + "\n"
	scripts["space 257th"] = "#!" + slashes(254) + " " + strings.Repeat("e", 50) + "\n"
	for _, n := range []int{300, 500, 501} {
		scripts["argument "+strconv.Itoa(n)] = "#!/bin/sh -" + strings.Repeat("e", n) + "\n"
	}
	for name, body := range scripts {
		plant(t, d, name, body, 0o755)
	}
	if err := syscall.Mkfifo(filepath.Join(d, "pipe"), 0o755); err != nil {
		t.Fatal(err)
	}
	started := 0
	for _, name := range append(slices.Sorted(maps.Keys(scripts)), "pipe") {
		p := filepath.Join(d, name)
		chain, err := Interpreters(p)
		cmd := exec.CommandContext(t.Context(), p) //nolint:gosec // the path is the test's own script, and starting it is the test
		cmd.Dir = d
		startErr := cmd.Start()
		if startErr == nil {
			started++
			_ = cmd.Wait() // the script's own exit is not the system's start
		}
		var errno syscall.Errno
		switch {
		case (err == nil) != (startErr == nil):
			t.Errorf("%s: Interpreters %q, %v, and the system's start %v", name, chain, err, startErr)
		case err != nil && (!errors.As(err, &errno) || !errors.Is(startErr, errno)):
			t.Errorf("%s: Interpreters refuses with %v, and the system with %v", name, err, startErr)
		case err == nil && (len(chain) == 0 || !filepath.IsAbs(chain[len(chain)-1])):
			t.Errorf("%s: the system starts it, and Interpreters answers %q", name, chain)
		}
	}
	if started < 8 {
		t.Errorf("the system started %d of the scripts, and want 8 or more", started)
	}
}

// What it costs when this fails: Interpreters opens a name a #! line chose before it knows the
// name is a regular file, so a device's driver runs its open, as a tape's rewinds.
// The pipe here no account but root may open: opened first, it is refused for its permission,
// and not for its type.
func TestInterpretersOpensNoNameThatIsNotARegularFile(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	shut := filepath.Join(d, "shut")
	if err := syscall.Mkfifo(shut, 0); err != nil {
		t.Fatal(err)
	}
	plant(t, d, "script", "#!"+shut+"\n", 0o755)
	for _, p := range []string{shut, filepath.Join(d, "script")} {
		if chain, err := Interpreters(p); !errors.Is(err, syscall.EACCES) || !strings.Contains(err.Error(), shut+" is of type p") || !strings.Contains(err.Error(), "not a regular file") {
			t.Errorf("%s: %q, %v, and want %s refused as a pipe", p, chain, err, shut)
		}
	}
}

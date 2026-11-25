package trust

import (
	"cmp"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// first is the first n bytes of s, zero past its end, as a system reads a file to its #! line.
func first(s string, n int) []byte {
	b := make([]byte, n)
	copy(b, s)
	return b
}

// What it costs when this fails: a program a file's #! line starts is not the one the caller
// walks, as when the line is read past where the system ends it, or a line the system cuts is
// read whole. Each row is proven by a real exec on Linux 7.0 and macOS 27.
func TestEachSystemReadsAShebangLineByItsOwnRule(t *testing.T) {
	t.Parallel()
	slashes := func(n int) string { return strings.Repeat("/", n-len("bin/sh")) + "bin/sh" }
	for _, c := range []struct {
		name, line, linux, darwin string // "" is refused
	}{
		{"a program", "#!/bin/sh\necho\n", "/bin/sh", "/bin/sh"},
		{"an argument", "#!/bin/sh -e\n", "/bin/sh", "/bin/sh"},
		{"an argument after a tab", "#!/bin/sh\t-e\n", "/bin/sh", "/bin/sh"},
		{"spaces and tabs before it", "#! \t /bin/sh\n", "/bin/sh", "/bin/sh"},
		{"spaces after it", "#!/bin/sh   \n", "/bin/sh", "/bin/sh"},
		{"env", "#!/usr/bin/env sh\n", "/usr/bin/env", "/usr/bin/env"},
		{"a relative name", "#!sh\n", "sh", "sh"},
		{"a carriage return", "#!/bin/sh\r\n", "/bin/sh\r", "/bin/sh\r"},
		{"a NUL", "#!/bin/sh\x00junk\n", "/bin/sh", "/bin/sh"},
		{"a NUL first", "#!\x00/bin/sh\n", "", ""},
		{"no newline", "#!/bin/sh", "/bin/sh", ""},
		{"nothing", "#!\n", "", ""},
		{"spaces", "#!   \n", "", ""},
		{"spaces to the end", "#!   ", "", ""},
		{"a hash after it", "#!/bin/sh#x\n", "/bin/sh#x", "/bin/sh"},
		{"a hash after its argument", "#!/bin/sh -e#x\n", "/bin/sh", "/bin/sh"},
		{"a hash before it", "#! #/bin/sh\n", "#/bin/sh", ""},
		{"Linux's longest, with its newline", "#!" + slashes(253) + "\n", slashes(253), slashes(253)},
		{"Linux's longest, with no newline", "#!" + slashes(252) + strings.Repeat("e", 600), "", ""},
		{"Linux's longest name, then a space", "#!" + slashes(252) + " " + strings.Repeat("e", 200) + "\n", slashes(252), slashes(252)},
		{"past Linux's longest", "#!" + slashes(254) + "\n", "", slashes(254)},
		{"an argument past Linux's longest", "#!/bin/sh -" + strings.Repeat("e", 300) + "\n", "/bin/sh", "/bin/sh"},
		{"a space at Linux's 256th byte", "#!" + slashes(253) + " " + strings.Repeat("e", 50) + "\n", slashes(253), slashes(253)},
		{"a space past Linux's 256th byte", "#!" + slashes(254) + " " + strings.Repeat("e", 50) + "\n", "", slashes(254)},
		{"macOS's longest", "#!" + slashes(509) + "\n", "", slashes(509)},
		{"an argument to macOS's longest", "#!/bin/sh -" + strings.Repeat("e", 500) + "\n", "/bin/sh", "/bin/sh"},
		{"an argument past macOS's longest", "#!/bin/sh -" + strings.Repeat("e", 501) + "\n", "/bin/sh", ""},
	} {
		for _, k := range []struct {
			system string
			rule   shebang
			want   string
		}{{"Linux", linuxShebang(), c.linux}, {"macOS", darwinShebang(), c.darwin}} {
			got, err := k.rule.line(first(c.line, k.rule.size))
			if got != k.want || (err == nil) != (k.want != "") || err != nil && !errors.Is(err, syscall.ENOEXEC) {
				t.Errorf("%s, on %s: %q, %v, and want %q", c.name, k.system, got, err, k.want)
			}
		}
	}
}

// plantScripts plants in d the program prog, which is no script, and the scripts s0, a #! line
// naming prog, and s1 to s5, each naming the one before; it fails where d cannot be written on a
// #! line.
func plantScripts(t *testing.T, d string) {
	t.Helper()
	if strings.ContainsAny(d, " \t#\n\x00") {
		t.Fatalf("the test folder %q holds a byte that ends a #! line, so no script here can name an interpreter in it: set TMPDIR to a folder without one", d)
	}
	plant(t, d, "prog", "no script\n", 0o755)
	for i, before := range []string{"prog", "s0", "s1", "s2", "s3", "s4"} {
		plant(t, d, "s"+string(rune('0'+i)), "#!"+filepath.Join(d, before)+" -x\n", 0o755)
	}
}

// plant writes body to the name in d, with mode.
func plant(t *testing.T, d, name, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(d, name), []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

// What it costs when this fails: a caller walks some of the programs a script starts and not the
// last, or a chain the system refuses is taken for one it runs.
func TestAChainOfScriptsIsWalkedToTheProgramTheSystemStarts(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	plantScripts(t, d)
	in := func(names ...string) []string {
		var p []string
		for _, n := range names {
			p = append(p, filepath.Join(d, n))
		}
		return p
	}
	plant(t, d, "rel", "#!prog\n", 0o755)
	plant(t, d, "rel here", "#!interpreters.go\n", 0o755) // a name in the folder the test runs in
	plant(t, d, "folder", "#!"+d+"\n", 0o755)
	plant(t, d, "missing", "#!"+filepath.Join(d, "none")+"\n", 0o755)
	for _, c := range []struct {
		name, path     string
		linux, darwin  []string
		err, darwinErr error // why Linux refuses, and macOS where it differs
	}{
		{"a program", "prog", []string{}, []string{}, nil, nil},
		{"a script", "s0", in("prog"), in("prog"), nil, nil},
		{"a script naming a script", "s1", in("s0", "prog"), nil, nil, syscall.ENOEXEC},
		{"Linux's longest chain", "s4", in("s3", "s2", "s1", "s0", "prog"), nil, nil, syscall.ENOEXEC},
		{"past Linux's longest chain", "s5", nil, nil, syscall.ELOOP, syscall.ENOEXEC},
		{"a relative interpreter", "rel", nil, nil, syscall.ENOENT, nil},
		{"a relative interpreter the folder a run is in holds", "rel here", nil, nil, syscall.ENOENT, nil},
		{"a folder for its interpreter", "folder", nil, nil, syscall.EACCES, nil},
		{"an interpreter not there", "missing", nil, nil, syscall.ENOENT, nil},
		{"a folder", ".", nil, nil, syscall.EACCES, nil},
		{"a name not there", "none", nil, nil, syscall.ENOENT, nil},
	} {
		for _, k := range []struct {
			system string
			rule   shebang
			want   []string
			err    error
		}{{"Linux", linuxShebang(), c.linux, c.err}, {"macOS", darwinShebang(), c.darwin, cmp.Or(c.darwinErr, c.err)}} {
			got, err := interpreters(filepath.Join(d, c.path), k.rule)
			if k.want != nil && (err != nil || !slices.Equal(got, k.want)) {
				t.Errorf("%s, on %s: %q, %v, and want %q", c.name, k.system, got, err, k.want)
			}
			if k.want == nil && (got != nil || !errors.Is(err, k.err)) {
				t.Errorf("%s, on %s: %q, %v, and want %v", c.name, k.system, got, err, k.err)
			}
		}
	}
	for _, p := range []string{"prog", "s0"} {
		if got, err := interpreters(filepath.Join(d, p), shebang{}); got != nil || err != nil {
			t.Errorf("%s, on Windows: %q, %v, and want none", p, got, err)
		}
	}
	if got, err := interpreters(filepath.Join(d, "prog"), unprovenShebang()); len(got) != 0 || err != nil {
		t.Errorf("a program, on another Unix: %q, %v, and want none", got, err)
	}
	if got, err := interpreters(filepath.Join(d, "s0"), unprovenShebang()); got != nil || !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("a script, on another Unix: %q, %v, and want errors.ErrUnsupported", got, err)
	}
}

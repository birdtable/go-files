package trust

import (
	"errors"
	"io/fs"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test builds the program of testdata/overflow with the go command and runs it; the module has no runner
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The tests of this file hold Who.Overflow's gates on every system: the uid_map read
// as numbers, the field refused off Linux and outside a user namespace that maps no root, and a
// program that is not a test binary refused. The namespace rows are in overflow_linux_test.go.

// What it costs when this fails: a namespace that maps root, or maps no account, takes the overflow
// account as root's, and every name of an account it does not map is trusted.
func TestNoRootTakesAUIDMapOfLinesThatMapNoRoot(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name, uidMap string
		err          error
		want         bool
	}{
		{"this account mapped, as --map-current-user maps it", "      1000       1000          1\n", nil, true},
		{"two lines, neither root's", "1000 1000 1\n100000 200000 65536\n", nil, true},
		{"no line, as a namespace with no map has", "", nil, false},
		{"root mapped to this account, as --map-root-user maps it", "         0       1000          1\n", nil, false},
		{"the first namespace's whole map", "         0          0 4294967295\n", nil, false},
		{"root on a second line", "1000 1000 1\n0 100000 65536\n", nil, false},
		{"a line of two numbers", "1000 1000\n", nil, false},
		{"an id below zero", "-1 1000 1\n", nil, false},
		{"an id that is no number", "x 1000 1\n", nil, false},
		{"an id past 32 bits", "4294967296 1000 1\n", nil, false},
		{"a map that could not be read", "1000 1000 1\n", fs.ErrPermission, false},
		{"the overflow id mapped to a subordinate id, as newuidmap maps one", "1000 1000 1\n65534 100000 1\n", nil, false},
		{"every id but root's mapped, the overflow id among them", "1 1 4294967294\n", nil, false},
		{"a range that ends just below the overflow id", "65533 100000 1\n", nil, true},
		{"a range that starts just past the overflow id", "65535 100000 1\n", nil, true},
	} {
		if got := (host{overflow: "uid:65534"}).noRoot([]byte(row.uidMap), row.err); got != row.want {
			t.Errorf("%s: noRoot(%q, %v) = %t, and want %t", row.name, row.uidMap, row.err, got, row.want)
		}
	}
	if (host{}).noRoot([]byte("1000 1000 1\n"), nil) {
		t.Error("noRoot passed a host with no overflow account to keep out of the map")
	}
}

// What it costs when this fails: a program run as nobody where every id is mapped, in the first
// namespace or a container with none of its own, is refused every call as at no tag before; or one
// run as the overflow account in a namespace that maps part of the ids, where it may be any account
// left out, is served.
func TestOverflowOfRefusesTheOverflowAccountOnlyWhereTheUIDMapIsNotTheWholeRange(t *testing.T) {
	t.Parallel()
	whole := "         0          0 4294967295\n"
	for _, row := range []struct {
		name, me, uid, uidMap string
		uidErr, mapErr        error
		refused               bool
	}{
		{"nobody where every id is mapped", "uid:65534", "65534\n", whole, nil, nil, false},
		{"another account, in a namespace that maps part", "uid:1000", "65534\n", "1000 1000 1\n", nil, nil, false},
		{"the overflow account, in a namespace that maps part", "uid:65534", "65534\n", "1000 1000 1\n", nil, nil, true},
		{"the overflow account, in a namespace that maps none", "uid:65534", "65534\n", "", nil, nil, true},
		{"the overflow account, every id but the last mapped", "uid:65534", "65534\n", "0 0 4294967294\n", nil, nil, true},
		{"the overflow account, where the map cannot be read", "uid:65534", "65534\n", whole, nil, fs.ErrPermission, true},
		{"an overflow account that cannot be read", "uid:1000", "", whole, fs.ErrNotExist, nil, true},
		{"an overflow account that is no number", "uid:1000", "x\n", whole, nil, nil, true},
	} {
		read := func(name string) ([]byte, error) {
			if name == "/proc/self/uid_map" {
				return []byte(row.uidMap), row.mapErr
			}
			if name != "overflowuid" {
				t.Errorf("%s: read %s", row.name, name)
			}
			return []byte(row.uid), row.uidErr
		}
		o, err := overflowOf(row.me, "overflowuid", read)
		if errors.Is(err, ErrOverflow) != row.refused || row.refused != (err != nil) {
			t.Errorf("%s: %v, and want refused %t, with ErrOverflow", row.name, err, row.refused)
		}
		if row.uidErr == nil && row.uid != "x\n" && o != "uid:65534" {
			t.Errorf("%s: the overflow account is %q, and want uid:65534", row.name, o)
		}
	}
}

// What it costs when this fails: Who.Overflow is taken off Linux, or outside a namespace that maps
// no root, or quietly ignored, and the walk trusts an account it was never asked to, or walks on.
func TestOverflowIsTakenOnlyInATestInANamespaceThatMapsNoRoot(t *testing.T) {
	t.Parallel()
	if local().noRoot(os.ReadFile("/proc/self/uid_map")) {
		t.Fatal("the test tier runs in a user namespace that maps no root; the rows here want one that does, or none")
	}
	nothing := host{look: func(p string) (node, error) {
		t.Errorf("a refused Who.Overflow walked to %s", p)
		return node{}, fs.ErrNotExist
	}, root: []string{rootID}, me: meID}
	broken := errors.New("this host cannot judge")
	for _, row := range []struct {
		name     string
		overflow string
		broken   error
		on       bool
		want     []error
		not      error
	}{
		{"unset", "uid:65534", nil, false, nil, nil},
		{"set off Linux, where no overflow account is read", "", nil, true, []error{ErrOverflow, errors.ErrUnsupported}, nil},
		{"set on Linux, outside a namespace that maps no root", "uid:65534", nil, true, []error{ErrOverflow}, errors.ErrUnsupported},
		{"set on a host that is broken already", "uid:65534", broken, true, []error{broken}, ErrOverflow},
	} {
		h := nothing
		h.overflow, h.broken = row.overflow, row.broken
		got := overflow(h, row.on)
		if !slices.Equal(got.root, []string{rootID}) {
			t.Errorf("%s: the owners trusted as root are %q, and want %q alone", row.name, got.root, rootID)
		}
		for _, want := range row.want {
			if !errors.Is(got.broken, want) {
				t.Errorf("%s: the host is broken by %v, and want it to match %v", row.name, got.broken, want)
			}
		}
		if row.want == nil && got.broken != nil || row.not != nil && errors.Is(got.broken, row.not) {
			t.Errorf("%s: the host is broken by %v", row.name, got.broken)
		}
		if row.on {
			if _, err := walk(at("a"), Folder, Who{Overflow: true}, h); err == nil {
				t.Errorf("%s: the walk passed", row.name)
			}
		}
	}
}

// What it costs when this fails: a program that sets Who.Overflow, which no program may, has the
// walk trust every account a user namespace does not map, or has it ignored and never knows.
func TestAProgramThatIsNotATestIsRefusedTheOverflowAccount(t *testing.T) {
	t.Parallel()
	prog, dir := program(t), ownFolder(t)
	out, err := exec.CommandContext(t.Context(), prog, dir).CombinedOutput() //nolint:gosec // G204: the program this test built, on the test's own folder
	if err != nil {
		t.Fatalf("%s: %v: %s", prog, err, out)
	}
	// Off Linux is asked first: there the field is refused as unsupported, test binary or not.
	want := "overflow=true refused=true ErrOverflow=true ErrUnsupported=false: the overflow account setting is set outside a test binary"
	if local().overflow == "" {
		want = "overflow=true refused=true ErrOverflow=true ErrUnsupported=true: the overflow account setting is taken on Linux alone"
	}
	if !strings.Contains(string(out), want) {
		t.Errorf("the program printed\n%s\nand want a line starting %q", out, want)
	}
}

// program builds testdata/overflow as a program is built, not as a test binary, and answers its path.
func program(t *testing.T) string {
	t.Helper()
	// go test's cache keys a passing run on what the test process opens, not on what go build reads,
	// so the test reads the program's folder itself: an open folder is keyed on each
	// of its files' size and time, and a run cached before the program changed runs again.
	if _, err := os.ReadDir(filepath.Join("testdata", "overflow")); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	prog := filepath.Join(dir, "overflow.exe")                                                // the name Windows runs, which the others run as well
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", prog, "./testdata/overflow") //nolint:gosec // G204: the go command, building this package's own testdata
	// The environment is testkit's, which pins the go command's module and build caches and GOPATH
	// where the run had them. The modules come from that cache, with GOPROXY=off, so
	// no run reaches the network, and the build reuses what the build cache holds, so the program
	// is not compiled from the standard library up on every run; GOPATH is the
	// test's own.
	cmd.Env = append(os.Environ(), "GOWORK=off", "CGO_ENABLED=0", "GOPROXY=off", //nolint:forbidigo // cli.config.3: the go command reads the environment the tests were started in
		"GOPATH="+filepath.Join(dir, "gopath"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./testdata/overflow: %v: %s", err, out)
	}
	return prog
}

// ownFolder is a new folder of the test's, with no link on its way.
func ownFolder(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

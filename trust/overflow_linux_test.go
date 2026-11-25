package trust

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test starts unshare, with this test binary again and the program of testdata/overflow in it; the module has no runner
	"slices"
	"strings"
	"testing"
)

// The namespace rows of Who.Overflow and of a walk by the overflow account.
// Each runs in a user namespace unshare makes: this test binary again for the
// namespaces that map this account, and the program of testdata/overflow for the one that maps
// none, where a test binary cannot start, as an account no namespace maps can make no file, and
// for one that maps this account as root, where /dev/null is mounted over the overflow account's
// file, so it reads as nothing: mount(8) takes no other account.
const nsTest = "TestAUserNamespaceTrustsTheOverflowAccountOnlyWhereItMapsThisAccountAndNotRoot"

// nsArg marks this test binary run again in a user namespace, before the namespace's name.
const nsArg = "trust-namespace"

// What it costs when this fails: a test in a user namespace cannot walk its own folder, whose top
// shows as the overflow account's; or the walk trusts every unmapped account in a namespace that
// maps root or none, or in a program that is not a test.
func TestAUserNamespaceTrustsTheOverflowAccountOnlyWhereItMapsThisAccountAndNotRoot(t *testing.T) {
	if i := slices.Index(os.Args, nsArg); i > 0 && i+1 < len(os.Args) {
		inNamespace(t, os.Args[i+1])
		return
	}
	t.Parallel()
	prog, dir := program(t), ownFolder(t)
	for _, row := range []struct {
		name  string
		flags []string
		want  []string
	}{
		{"mapped", []string{"--user", "--map-current-user"}, []string{
			"overflow=false refused=true ErrOverflow=false ErrUnsupported=false: / is owned by uid:",
			"overflow=true refused=true ErrOverflow=true ErrUnsupported=false: the overflow account setting is set outside a test binary",
			"Account ErrOverflow=false ReadOwned ErrOverflow=false",
		}},
		{"root", []string{"--user", "--map-root-user"}, nil},
		{"unread", []string{"--user", "--map-root-user", "--mount", "sh", "-c", `mount --bind /dev/null /proc/sys/kernel/overflowuid && exec "$0" "$@"`}, []string{
			"overflow=false refused=true ErrOverflow=true ErrUnsupported=false: the overflow account cannot be read from /proc/sys/kernel/overflowuid",
			"overflow=true refused=true ErrOverflow=true ErrUnsupported=false: the overflow account cannot be read from /proc/sys/kernel/overflowuid",
			"Account ErrOverflow=true ReadOwned ErrOverflow=true",
		}},
		{"unmapped", []string{"--user"}, []string{
			"overflow=false refused=true ErrOverflow=true ErrUnsupported=false: this account is uid:",
			"overflow=true refused=true ErrOverflow=true ErrUnsupported=false: this account is uid:",
			"Account ErrOverflow=true ReadOwned ErrOverflow=true",
		}},
	} {
		if row.name == "mapped" || row.name == "root" {
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			args := append(row.flags, exe, "-test.run=^"+nsTest+"$", "-test.count=1", "-test.v", nsArg, row.name)
			out, err := exec.CommandContext(t.Context(), "unshare", args...).CombinedOutput() //nolint:gosec // G204: unshare and this test binary, with the row's literal flags
			if err != nil || !strings.Contains(string(out), "the namespace rows ran: "+row.name) {
				t.Errorf("%s: this test in unshare %s: %v:\n%s", row.name, strings.Join(row.flags, " "), err, out)
			}
		}
		if row.want == nil {
			continue
		}
		out, err := exec.CommandContext(t.Context(), "unshare", append(row.flags, prog, dir)...).CombinedOutput() //nolint:gosec // G204: unshare and the program this test built, with the row's literal flags
		if err != nil {
			t.Fatalf("%s: the program in unshare %s: %v: %s", row.name, strings.Join(row.flags, " "), err, out)
		}
		for _, want := range row.want {
			if !strings.Contains(string(out), want) {
				t.Errorf("%s: the program in unshare %s printed\n%s\nand want a line starting %q", row.name, strings.Join(row.flags, " "), out, want)
			}
		}
	}
}

// inNamespace is the test's rows inside the namespace of mode, which the test made with unshare:
// "mapped", this account and no other, or "root", this account mapped as root.
func inNamespace(t *testing.T, mode string) {
	t.Helper()
	dir := ownFolder(t)
	_, plain := Path(dir, Folder, Who{})
	_, asked := Path(dir, Folder, Who{Overflow: true})
	switch mode {
	case "mapped":
		if !errors.Is(plain, ErrNotOwners) || asked != nil {
			t.Errorf("this test's folder %s: %v with Who{}, and want ErrNotOwners; %v with Who.Overflow, and want a pass", dir, plain, asked)
		}
		overflowAsRoot(t)
	case "root":
		if !errors.Is(plain, ErrNotOwners) || !errors.Is(asked, ErrOverflow) {
			t.Errorf("this test's folder %s: %v with Who{}, and want ErrNotOwners; %v with Who.Overflow, and want ErrOverflow", dir, plain, asked)
		}
	default:
		t.Fatalf("no namespace rows are named %q", mode)
	}
	t.Logf("the namespace rows ran: %s", mode)
}

// overflowAsRoot holds, in a namespace that maps this account and not root, that Who.Overflow
// trusts the overflow account as an owner, as root is trusted, and changes no other rule: another
// account's name, a folder its group can write and a name right in its sticky folder are judged
// as they are with root's.
func overflowAsRoot(t *testing.T) {
	t.Helper()
	o := local().overflow
	b, err := os.ReadFile("/proc/sys/kernel/overflowgid")
	var gid uint32
	if _, scan := fmt.Sscan(string(b), &gid); err != nil || scan != nil {
		t.Fatalf("the overflow group: %v, %v", err, scan)
	}
	if o == "" || o == rootID {
		t.Fatalf("the overflow account read is %q", o)
	}
	f := disk(map[string]node{
		top():           dir(o, 0o755),
		at("a"):         dir(o, 0o755),
		at("b"):         dir(otherID, 0o755),
		at("g"):         {typ: fs.ModeDir, owner: o, perm: 0o775, gid: gid},
		at("t"):         dir(o, fs.ModeSticky|0o777),
		at("t", "mine"): dir(meID, 0o700),
		at("t", "r"):    dir(o, 0o755),
		at("t", "x"):    file(otherID, 0o644),
	})
	h := f.host(meID)
	h.overflow = o
	for _, row := range []struct {
		path string
		kind Kind
		who  Who
		want error
	}{
		{at("a"), Folder, Who{}, ErrNotOwners},
		{at("a"), Folder, Who{Overflow: true}, nil},
		{at("a"), Folder, Who{Overflow: true, RootOnly: true}, nil},
		{at("b"), Folder, Who{Overflow: true}, ErrNotOwners},
		{at("g"), Folder, Who{Overflow: true}, ErrOthersWrite},
		{at("g"), Folder, Who{Overflow: true, Group: func(g uint32) bool { return g == gid }}, nil},
		{at("t", "mine"), Folder, Who{Overflow: true}, nil},
		{at("t", "x"), File, Who{Overflow: true}, ErrNotOwners},
		{at("t", "r"), Folder, Who{Overflow: true}, ErrNotOwners},
	} {
		_, err := walk(row.path, row.kind, row.who, h)
		if row.want == nil && err != nil || row.want != nil && !errors.Is(err, row.want) {
			t.Errorf("%s with %+v: %v, and want %v", row.path, row.who, err, row.want)
		}
	}
}

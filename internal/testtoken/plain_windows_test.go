//go:build windows

package testtoken

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// TestPlainLeavesNeitherPrivilegeEnabled reads what the process's token says of SeBackupPrivilege
// and SeRestorePrivilege, calls Plain, and reads it again, and logs both reads, a line for each
// privilege: run alone with -v, as the release workflow's Windows job runs it, the log says what
// the job's token gave its processes. The package has no TestMain, so
// the first read is of the token as the test process was handed it. After Plain neither is
// enabled, one it found enabled is still held, and one it did not find enabled is as it was.
//
// What it costs when this fails: the tests of a session whose token has a privilege enabled, as a
// hosted runner's may, read a file they were denied or name an owner no token holds, and two
// refusals pass there that hold everywhere else; or Plain takes from a token what it should leave.
func TestPlainLeavesNeitherPrivilegeEnabled(t *testing.T) {
	t.Parallel()
	want, err := two()
	if err != nil {
		t.Fatal(err)
	}
	before, err := read(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := Plain(); err != nil {
		t.Fatalf("Plain: %v, and want nil", err)
	}
	after, err := read(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != 2 || len(before) != 2 || len(after) != 2 {
		t.Fatalf("read %d privileges before and %d after, of %d, and want 2 each", len(before), len(after), len(want))
	}
	for i, p := range want {
		t.Logf("%s: %s before Plain, %s after", p.name, before[i], after[i])
		switch {
		case after[i] == enabled:
			t.Errorf("%s is enabled after Plain", p.name)
		case before[i] == enabled && after[i] != off:
			t.Errorf("%s was enabled and is %s after Plain, and want it held and not enabled", p.name, after[i])
		case before[i] != enabled && after[i] != before[i]:
			t.Errorf("%s was %s and is %s after Plain, which changes only a privilege it finds enabled", p.name, before[i], after[i])
		}
	}
	if err := Plain(); err != nil {
		t.Errorf("Plain, called again: %v, and want nil", err)
	}
}

// TestOnlyAPrivilegeHeldEnabledIsDisabledAndOneLeftEnabledIsTheError runs plain on a privilege
// the test enables itself, since no session's token is sure to have either of Plain's two
// enabled: the first the token holds and has not enabled, other than those two (spareOf). With a
// disable that does nothing the privilege stays enabled and plain says so by its name; a
// disable that fails gives plain its error; the package's own disable leaves it held and not
// enabled; and plain then asks nothing more of it, nor of a privilege the token does not hold.
//
// What it costs when this fails: Plain answers nil with a privilege still enabled, and the two
// refusals it is for pass again on a hosted runner with nothing said; or it changes a privilege
// that was not enabled, in a token Plain leaves as it is.
func TestOnlyAPrivilegeHeldEnabledIsDisabledAndOneLeftEnabledIsTheError(t *testing.T) {
	t.Parallel()
	spare := spareOf(t)
	t.Cleanup(func() {
		if err := disable(spare); err != nil {
			t.Errorf("putting %s back: %v", spare.name, err)
		}
	})
	enable(t, spare)
	one := []privilege{spare}
	if got, err := read(one); err != nil || got[0] != enabled {
		t.Fatalf("the test enabled %s and the token says %v (%v), so no row below reads a privilege enabled", spare.name, got, err)
	}

	calls := 0
	idle := func(privilege) error { calls++; return nil }
	if _, _, err := plain(one, idle); err == nil || !strings.Contains(err.Error(), spare.name) || calls != 1 {
		t.Errorf("plain with a disable that does nothing: %v after %d calls, and want an error naming %s after 1", err, calls, spare.name)
	}
	refused := errors.New("refused")
	if _, _, err := plain(one, func(privilege) error { return refused }); !errors.Is(err, refused) {
		t.Errorf("plain with a disable that fails: %v, and want its error", err)
	}
	before, after, err := plain(one, disable)
	if err != nil || len(before) != 1 || len(after) != 1 || before[0] != enabled || after[0] != off {
		t.Fatalf("plain on %s: %v before, %v after (%v), and want it enabled before, held and not enabled after", spare.name, before, after, err)
	}

	calls = 0
	nowhere := privilege{name: "a privilege no token holds", luid: windows.LUID{LowPart: 0xffffffff, HighPart: 0x7fffffff}}
	before, after, err = plain([]privilege{spare, nowhere}, idle)
	if err != nil || calls != 0 || !slices.Equal(before, []state{off, absent}) || !slices.Equal(after, before) {
		t.Errorf("plain on a privilege not enabled and one not held: %v before, %v after, %d calls (%v), and want both left as they were with no call", before, after, calls, err)
	}
}

// spareOf is the first privilege the process's token holds and has not enabled, other than
// Plain's two, named by its number. A token with none fails the test: no row could enable one.
func spareOf(t *testing.T) privilege {
	t.Helper()
	list, err := held()
	if err != nil {
		t.Fatal(err)
	}
	two, err := two()
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range list {
		if h.Attributes&windows.SE_PRIVILEGE_ENABLED == 0 && !slices.ContainsFunc(two, func(p privilege) bool { return p.luid == h.Luid }) {
			return privilege{name: fmt.Sprintf("the privilege numbered %d", h.Luid.LowPart), luid: h.Luid}
		}
	}
	t.Fatalf("the process's token holds %d privileges and none the test may enable, so no row runs", len(list))
	return privilege{}
}

// enable enables p in the process's own token, which may enable any privilege it holds.
func enable(t *testing.T, p privilege) {
	t.Helper()
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY|windows.TOKEN_ADJUST_PRIVILEGES, &token); err != nil {
		t.Fatalf("opening the process's token to enable %s: %v", p.name, err)
	}
	defer func() { _ = token.Close() }()
	to := windows.Tokenprivileges{PrivilegeCount: 1, Privileges: [1]windows.LUIDAndAttributes{{Luid: p.luid, Attributes: windows.SE_PRIVILEGE_ENABLED}}}
	if err := windows.AdjustTokenPrivileges(token, false, &to, 0, nil, nil); err != nil {
		t.Fatalf("enabling %s: %v", p.name, err)
	}
}

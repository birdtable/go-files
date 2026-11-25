//go:build unix

package appdirs

import (
	"errors"
	"os"
	"os/user"
	"strings"
	"testing"
)

// A uid no account has is ErrNoAccountHome through the lookup AccountHome makes, and no home is
// answered. Where the build answers the running account's home, the cause is os/user's
// UnknownUserIdError; a build that answers none, macOS's with osusergo, refuses every uid alike.
//
// What it costs when this fails: a process whose uid the account records do not hold, as in a
// container run under an arbitrary uid, is answered a home anyway, from somewhere other than the
// records.
func TestOnUnixAnUnknownUIDIsErrNoAccountHome(t *testing.T) {
	t.Parallel()
	const uid = 2147483645 // below the 32-bit ids' end, where nobody and nfsnobody sit, and in no account database
	got, err := homeOf(byUID(uid))
	_, own := AccountHome()
	var unknown user.UnknownUserIdError
	if got != "" || !errors.Is(err, ErrNoAccountHome) || own == nil && !errors.As(err, &unknown) {
		t.Errorf("uid %d gives %q, %v, want ErrNoAccountHome with os/user's UnknownUserIdError", uid, got, err)
	}
}

// AccountHome's own read of /etc/passwd, in the builds where os/user would fall back to HOME,
// answers the home field of the first line whose uid field is the uid, skipping NIS's lines, and
// refuses a uid no line has, a line of the uid that is not seven fields, an empty or relative home,
// and a read that fails, each as ErrNoAccountHome.
//
// What it costs when this fails: a build without cgo answers another account's home, a field that
// is not the home, or "" or a relative path, as the running account's own.
func TestOnUnixPasswdIsReadForTheUIDsOwnHomeAlone(t *testing.T) {
	t.Parallel()
	const head = "root:x:0:0:root:/root:/bin/sh\n+u::1000:1000::/nis:/bin/sh\n"
	for _, row := range []struct {
		name, passwd, want string
		unknown            bool
	}{
		{"its line", head + "u:x:1000:1000:U:/home/u:/bin/sh\nv:x:1000:1000:V:/home/v:/bin/sh\n", "/home/u", false},
		{"a name's field that is the uid", head + "1000:x:1001:1001::/home/n:/bin/sh\n", "", true},
		{"no line of the uid", head, "", true},
		{"a line of six fields", head + "u:x:1000:1000:U:/home/u\n", "", false},
		{"a line of eight fields", head + "u:x:1000:1000:U:/home/u:/bin/sh:x\n", "", false},
		{"an empty home", head + "u:x:1000:1000:U::/bin/sh\n", "", false},
		{"a relative home", head + "u:x:1000:1000:U:home/u:/bin/sh\n", "", false},
		{"a line longer than a read takes", strings.Repeat("x", 1<<17) + "\nu:x:1000:1000:U:/home/u:/bin/sh\n", "", false},
	} {
		got, err := homeOf(homeInPasswd(strings.NewReader(row.passwd), 1000))
		var unknown user.UnknownUserIdError
		if row.want != "" && (got != row.want || err != nil) ||
			row.want == "" && (got != "" || !errors.Is(err, ErrNoAccountHome) || errors.As(err, &unknown) != row.unknown) {
			t.Errorf("%s: uid 1000 gives %q, %v, want %q, or ErrNoAccountHome (UnknownUserIdError %t)", row.name, got, err, row.want, row.unknown)
		}
	}
}

// accountWithoutCgo is what AccountHome answers in a build with the osusergo tag and without cgo,
// not read from os/user: ErrNoAccountHome on macOS, and /etc/passwd's home for the uid, as
// homeInPasswd reads it, on the other Unix systems.
func accountWithoutCgo(t *testing.T) (string, error) {
	t.Helper()
	if thisKind == darwinKind {
		return "", ErrNoAccountHome
	}
	f, err := os.Open("/etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	return homeOf(homeInPasswd(f, os.Getuid()))
}

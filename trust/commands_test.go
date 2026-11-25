package trust

import (
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"
)

// What it costs when this fails: a person pastes a command that changes the wrong name, or
// changes more than the fix asks, or a fix no one command makes is shown as one, so the next
// check refuses again; or a word of a path is read as a switch; or the line that
// gives a name to this account gives it to another, or to no one the system knows.
func TestAFixIsTheStepsThatMakeItAsArgv(t *testing.T) {
	t.Parallel()
	const p, w = "/srv/a b/$x", `C:\Program Files\x$`
	const userSID = "S-1-5-21-1004336348-1177238915-682003330-1001"
	for _, c := range []struct {
		name string
		fix  Fix
		want []Step
	}{
		{"others' write", Fix{Path: p, Clear: 0o022}, []Step{{Argv: []string{"chmod", "go-w", p}}}},
		{"its group's write", Fix{Path: p, Clear: 0o020}, []Step{{Argv: []string{"chmod", "g-w", p}}}},
		{"a key's group and others", Fix{Path: p, Clear: 0o077}, []Step{{Argv: []string{"chmod", "go-rwx", p}}}},
		{"a private file's", Fix{Path: p, Clear: 0o066}, []Step{{Argv: []string{"chmod", "go-rw", p}}}},
		{"a program's run bit", Fix{Path: p, Set: 0o100}, []Step{{Argv: []string{"chmod", "u+x", p}}}},
		{"cleared and set", Fix{Path: p, Clear: 0o002, Set: 0o500}, []Step{{Argv: []string{"chmod", "o-w,u+rx", p}}}},
		{"root's", Fix{Path: p, Owner: "uid:0"}, []Step{{Argv: []string{"chown", "-h", "root", p}, Admin: true}}},
		{"a mode, then root's", Fix{Path: p, Owner: "uid:0", Clear: 0o022},
			[]Step{{Argv: []string{"chmod", "go-w", p}}, {Argv: []string{"chown", "-h", "root", p}, Admin: true}}},
		{"Administrators'", Fix{Path: w, Owner: administrators}, []Step{{Argv: []string{"icacls", w, "/setowner", "*" + administrators, "/L"}, Admin: true}}},
		{"a SID's grant, then Administrators'", Fix{Path: w, Owner: administrators, Revoke: evilSID},
			[]Step{{Argv: []string{"icacls", w, "/inheritance:d", "/L"}}, {Argv: []string{"icacls", w, "/remove:g", "*" + evilSID, "/L"}},
				{Argv: []string{"icacls", w, "/setowner", "*" + administrators, "/L"}, Admin: true}}},
		{"a null list, then Administrators'", Fix{Path: w, Owner: administrators, Reset: true},
			[]Step{{Argv: []string{"icacls", w, "/reset", "/L"}}, {Argv: []string{"icacls", w, "/setowner", "*" + administrators, "/L"}, Admin: true}}},
		{"a null list", Fix{Path: w, Reset: true}, []Step{{Argv: []string{"icacls", w, "/reset", "/L"}}}},
		{"a share's", Fix{Path: `\\host\share\x`, Reset: true}, []Step{{Argv: []string{"icacls", `\\host\share\x`, "/reset", "/L"}}}},
		{"a SID's grant", Fix{Path: w, Revoke: evilSID},
			[]Step{{Argv: []string{"icacls", w, "/inheritance:d", "/L"}}, {Argv: []string{"icacls", w, "/remove:g", "*" + evilSID, "/L"}}}},
		{"none", Fix{}, nil},
		{"no path", Fix{Clear: 0o022}, nil},
		{"nothing to change", Fix{Path: p}, nil},
		{"a relative Unix path", Fix{Path: "-R", Clear: 0o022}, nil},
		{"a relative Windows path", Fix{Path: "/reset", Reset: true}, nil},
		{"a drive-relative Windows path", Fix{Path: `C:x`, Reset: true}, nil},
		{"a path with a NUL", Fix{Path: "/a\x00b", Clear: 0o022}, nil},
		{"the sticky bit as a number", Fix{Path: p, Clear: 0o1000}, nil},
		{"the sticky bit", Fix{Path: p, Clear: fs.ModeSticky}, nil},
		{"a path that starts with a dash", Fix{Path: "/-R", Clear: 0o022}, []Step{{Argv: []string{"chmod", "go-w", "/-R"}}}},
		{"a Windows path that starts with a slash", Fix{Path: `\x`, Reset: true}, nil},
		{"a mode beyond the nine bits", Fix{Path: p, Set: 0o4000 | 1<<31}, nil},
		{"an account's", Fix{Path: p, Owner: "uid:501"}, []Step{{Argv: []string{"chown", "-h", "501", p}, Admin: true}}},
		{"a mode, then an account's", Fix{Path: p, Owner: "uid:7", Clear: 0o022},
			[]Step{{Argv: []string{"chmod", "go-w", p}}, {Argv: []string{"chown", "-h", "7", p}, Admin: true}}},
		{"the largest uid", Fix{Path: p, Owner: "uid:4294967294"}, []Step{{Argv: []string{"chown", "-h", "4294967294", p}, Admin: true}}},
		{"a user SID's", Fix{Path: w, Owner: userSID}, []Step{{Argv: []string{"icacls", w, "/setowner", "*" + userSID, "/L"}, Admin: true}}},
		{"a null list, then a user SID's", Fix{Path: w, Owner: userSID, Reset: true},
			[]Step{{Argv: []string{"icacls", w, "/reset", "/L"}}, {Argv: []string{"icacls", w, "/setowner", "*" + userSID, "/L"}, Admin: true}}},
		{"a user SID's on a share", Fix{Path: `\\host\share\x`, Owner: userSID},
			[]Step{{Argv: []string{"icacls", `\\host\share\x`, "/setowner", "*" + userSID, "/L"}, Admin: true}}},
		{"an owner by its name", Fix{Path: p, Owner: "bob"}, nil},
		{"an owner by its name, with a mode", Fix{Path: p, Owner: "bob", Clear: 0o022}, nil},
		{"a uid with a leading zero", Fix{Path: p, Owner: "uid:07"}, nil},
		{"a uid with no number", Fix{Path: p, Owner: "uid:"}, nil},
		{"a negative uid", Fix{Path: p, Owner: "uid:-1"}, nil},
		{"a uid of eleven digits", Fix{Path: p, Owner: "uid:12345678901"}, nil},
		{"a uid with a switch after it", Fix{Path: p, Owner: "uid:7 -R"}, nil},
		{"a gid", Fix{Path: p, Owner: "gid:7"}, nil},
		{"an owner SID with words after it", Fix{Path: w, Owner: userSID + " /grant Everyone:F"}, nil},
		{"an owner SID in lower case", Fix{Path: w, Owner: "s-1-5-32-544"}, nil},
		{"an account's on a Windows path", Fix{Path: w, Owner: "uid:7"}, nil},
		{"a user SID's on a Unix path", Fix{Path: p, Owner: userSID}, nil},
		{"a user SID's and a mode", Fix{Path: w, Owner: userSID, Clear: 0o022}, nil},
		{"a macOS revoke", Fix{Path: p, Revoke: "uid:7"}, nil},
		{"an NFSv4 revoke", Fix{Path: p, Revoke: "nfs4:bob@example"}, nil},
		{"a SID with words after it", Fix{Path: w, Revoke: evilSID + " /grant Everyone:F"}, nil},
		{"Windows's reset and Unix's mode", Fix{Path: w, Reset: true, Clear: 0o022}, nil},
		{"root's and a SID's", Fix{Path: p, Owner: "uid:0", Revoke: evilSID}, nil},
		{"Administrators' on a Unix path", Fix{Path: p, Owner: administrators}, nil},
		{"root's on a Windows path", Fix{Path: w, Owner: "uid:0"}, nil},
	} {
		got, ok := c.fix.Commands()
		if ok != (c.want != nil) || !slices.EqualFunc(got, c.want, func(a, b Step) bool { return a.Admin == b.Admin && slices.Equal(a.Argv, b.Argv) }) {
			t.Errorf("%s: %s, %v, and want %s", c.name, shown(got), ok, shown(c.want))
		}
	}
}

// shown is steps with each argv word quoted, so a word that holds a space shows as one.
func shown(steps []Step) string {
	var b strings.Builder
	for _, s := range steps {
		fmt.Fprintf(&b, "%q admin %v; ", s.Argv, s.Admin)
	}
	return b.String()
}

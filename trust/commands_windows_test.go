//go:build windows

package trust

import (
	"errors"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test runs each step of a fix as its argv, icacls, as a person runs them; the module has no runner
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// runFix starts each step of fix's commands as its argv, with no shell, and says whether it ran
// them: a step that needs an elevated prompt runs only from one.
func runFix(t *testing.T, fix Fix) bool {
	t.Helper()
	steps, ok := fix.Commands()
	if !ok {
		t.Fatalf("%+v: no commands", fix)
	}
	for _, s := range steps {
		if s.Admin && !windows.GetCurrentProcessToken().IsElevated() {
			t.Logf("%q needs an elevated prompt, and this is not one: not run", s.Argv)
			return false
		}
	}
	for _, s := range steps {
		if out, err := exec.CommandContext(t.Context(), s.Argv[0], s.Argv[1:]...).CombinedOutput(); err != nil { //nolint:gosec // the argv is Commands' own, and running it is the test
			t.Fatalf("%q: %v: %s", s.Argv, err, out)
		}
	}
	return true
}

// adminStep says whether this is an elevated prompt, and fails the test unless each step of fix
// needs one, as an owner's does. Where it is not elevated no step is run: the steps need an
// elevated shell, which no session has.
func adminStep(t *testing.T, fix Fix) bool {
	t.Helper()
	steps, ok := fix.Commands()
	if !ok || len(steps) == 0 {
		t.Fatalf("%+v: no commands", fix)
	}
	for _, s := range steps {
		if !s.Admin {
			t.Errorf("%q: Admin false, and want an owner's step to need an elevated prompt", s.Argv)
		}
	}
	if windows.GetCurrentProcessToken().IsElevated() {
		return true
	}
	t.Logf("%+v: its steps need an elevated prompt, and this is not one: not run here, since no session has an elevated shell", fix)
	return false
}

// What it costs when this fails: a person runs the icacls steps a refusal's fix gives,
// and the name is still refused, as when an inherited grant comes back because the list still
// inherits; or a name that holds a space, a dollar or a percent is split or expanded, so another
// name is changed.
func TestEachStepOfAFixRunAsItsArgvOnWindowsMakesTheNamePass(t *testing.T) {
	t.Parallel()
	d := nativeDir(t)
	split := filepath.Join(d, "a")
	newFile(t, split)
	setList(t, split, grant(me(t), full), grant(everyone, windows.FILE_GENERIC_WRITE))
	direct := filepath.Join(d, "a b $x %PATH%")
	newFile(t, direct)
	setList(t, direct, grant(me(t), full), grant(everyone, windows.FILE_GENERIC_WRITE))
	held := filepath.Join(d, "inherits")
	if err := os.Mkdir(held, 0o700); err != nil {
		t.Fatal(err)
	}
	// The folder's grant to this account passes to the file, as a folder's does under lockedDir's:
	// with a grant on the folder alone the file's one grant is the inherited Everyone, and once its
	// steps remove that, an account that is not elevated can no longer open the file to judge it.
	setList(t, held, entry{mode: windows.GRANT_ACCESS, sid: me(t), mask: full, inherit: windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT},
		entry{mode: windows.GRANT_ACCESS, sid: everyone, mask: windows.FILE_GENERIC_WRITE,
			inherit: windows.SUB_OBJECTS_ONLY_INHERIT | windows.INHERIT_ONLY})
	inherited := filepath.Join(held, "f $y")
	newFile(t, inherited)
	null := filepath.Join(d, "null %x%")
	newFile(t, null)
	if err := windows.SetNamedSecurityInfo(null, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, path string
		fix        Fix
	}{
		{"a grant to Everyone", direct, removeGrant(direct, everyone)},
		{"a grant to Everyone it inherits", inherited, removeGrant(inherited, everyone)},
		{"a null list", null, Fix{Path: null, Reset: true}},
	} {
		_, err := Path(c.path, File, Who{})
		wantRefused(t, c.name, err, refusal{want: ErrOthersWrite, fix: c.fix})
		runFix(t, c.fix)
		if _, err := Path(c.path, File, Who{}); err != nil {
			t.Errorf("%s after %+v: %v", c.name, c.fix, err)
		}
	}
	if _, err := Path(held, Folder, Who{}); err != nil {
		t.Errorf("the folder whose grant the file inherited, after the file's steps: %v", err)
	}
	if _, err := Path(split, File, Who{}); !errors.Is(err, ErrOthersWrite) {
		t.Errorf("a, a word of a name a step named: %v, and want it still refused", err)
	}
	mine := filepath.Join(d, "mine $z")
	newFile(t, mine)
	f, err := os.Open(mine)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fix := Fix{Path: mine, Owner: administrators}
	wantRefused(t, "this account's held, Administrators' alone trusted", Held(f, File, Who{RootOnly: true}), refusal{want: ErrNotOwners, says: "Path gives the name's"})
	if err := Held(f, File, Who{}); err != nil {
		t.Errorf("this account's held, this account trusted: %v, and want its owner its one refusal", err)
	}
	if adminStep(t, fix) && runFix(t, fix) {
		if owner, err := OwnerOf(f); err != nil || owner != administrators {
			t.Errorf("after icacls /setowner: owned by %q, %v, and want %s", owner, err, administrators)
		}
	}
}

// What it costs when this fails: a person runs the steps of the fix for a link's own list or
// owner (rule 5), and they change the folder the link names instead: a protected list reset to
// what it inherits from the folder above, which on a data drive lets every account change it, or
// its grants removed, or its owner changed, while the link itself is refused still.
func TestEachStepOfAFixForALinkOnWindowsChangesTheLinkAndNotWhatItNames(t *testing.T) {
	t.Parallel()
	d := nativeDir(t)
	target := filepath.Join(d, "keys")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	setList(t, target, entry{mode: windows.GRANT_ACCESS, sid: me(t), mask: full, inherit: windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT})
	f := filepath.Join(target, "f")
	newFile(t, f)
	before := ownerAndList(t, target)
	links := map[string]string{"a junction": filepath.Join(d, "j $x")}
	junction(t, links["a junction"], target)
	s := filepath.Join(d, "s %PATH%")
	switch err := os.Symlink(target, s); {
	case err == nil:
		links["a symbolic link"] = s
	case errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD):
		t.Logf("this account may not make a symbolic link, so a junction alone is planted: %v", err)
	default:
		t.Fatal(err)
	}
	for kind, l := range links {
		for _, c := range []struct {
			name, sddl string
			fix        Fix
		}{
			{"a null list", "D:PNO_ACCESS_CONTROL", Fix{Path: l, Reset: true}},
			{"Everyone let write", "D:P(A;;FA;;;" + me(t) + ")(A;;FA;;;WD)", removeGrant(l, everyone)},
		} {
			setOwnList(t, l, c.sddl)
			_, err := Path(filepath.Join(l, "f"), File, Who{})
			wantRefused(t, kind+" with "+c.name, err, refusal{want: ErrOthersWrite, fix: c.fix})
			runFix(t, c.fix)
			if got, err := Path(filepath.Join(l, "f"), File, Who{}); err != nil || got != f {
				t.Errorf("%s with %s, after its steps: %q, %v, and want %s", kind, c.name, got, err, f)
			}
			if after := ownerAndList(t, target); after != before {
				t.Errorf("%s with %s: the steps changed the folder it names, from %s to %s", kind, c.name, before, after)
			}
		}
		if fix := (Fix{Path: l, Owner: administrators}); adminStep(t, fix) && runFix(t, fix) {
			if n, err := look(l); err != nil || n.owner != administrators {
				t.Errorf("%s after icacls /setowner /L: owned by %q, %v, and want %s", kind, n.owner, err, administrators)
			}
			if after := ownerAndList(t, target); after != before {
				t.Errorf("%s: its owner step changed the folder it names, from %s to %s", kind, before, after)
			}
		}
	}
}

// ownerAndList is the owner and access list of the folder p, read by its name, as SDDL.
func ownerAndList(t *testing.T, p string) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return sd.String()
}

// What it costs when this fails: a person runs the line a program shows to give a name to this
// account, and icacls refuses the SID's form or gives the name to another account.
// Where it is not elevated the step runs on a name this account owns already, whose list grants
// it full control and so the right to take ownership. /L on a link is proved by the link test above.
func TestTheStepThatGivesANameToThisAccountRunsOnWindows(t *testing.T) {
	t.Parallel()
	p := filepath.Join(nativeDir(t), "mine $x %PATH%")
	newFile(t, p)
	setList(t, p, grant(me(t), full))
	steps, ok := Fix{Path: p, Owner: me(t)}.Commands()
	if !ok || len(steps) != 1 || !steps[0].Admin {
		t.Fatalf("%s, %v, and want one step that needs an elevated prompt", shown(steps), ok)
	}
	s := steps[0].Argv
	if out, err := exec.CommandContext(t.Context(), s[0], s[1:]...).CombinedOutput(); err != nil { //nolint:gosec // the argv is Commands' own, and running it is the test
		t.Fatalf("%q: %v: %s", s, err, out)
	}
	if n, err := look(p); err != nil || n.owner != me(t) {
		t.Errorf("after %q: owned by %q, %v, and want %s", s, n.owner, err, me(t))
	}
}

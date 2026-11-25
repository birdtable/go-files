package trust

import (
	"io/fs"
	"regexp"
	"strings"
)

// Step is one command that makes part of a Fix, as the words a program is started with: Argv[0]
// is the program, chmod, chown or icacls, and each word is one argument as it is passed, never
// quoted or joined. A caller that shows a step as a line to paste quotes each word for the shell
// it names. Admin is whether only an administrator can make the step, root on Unix and an elevated
// prompt on Windows, whoever owns the name: trust adds no sudo word, so the caller says so in its
// own. A step whose Admin is false is one the name's owner can make.
type Step struct {
	Argv  []string
	Admin bool
}

// winAbs is an absolute Windows path, on a drive or a share, that no Windows program reads as one
// of its switches.
var winAbs = regexp.MustCompile(`^([A-Za-z]:\\|\\\\)`)

// sidForm is a SID as a Windows Fix names one.
var sidForm = regexp.MustCompile(`^S-1(-[0-9]+)+$`)

// Commands is f as the steps that make it, in order, with ok true, or no steps and ok false when
// no command here makes the whole of it: the zero Fix; a path that is not absolute in the form of
// the system the fix is for, or that holds a NUL; mode bits beyond the nine; an owner not in
// Account's form; a revoke from a macOS or an NFSv4 list, whose entries take more than a name to
// remove; and a fix that mixes Windows's changes with Unix's. What the fix holds decides the
// system its commands are for, not the system this runs on: a reset and a SID, as an owner or a
// revoke, are Windows's, the mode bits and a uid Unix's. Nothing in trust quotes or joins a step:
// a Windows argv is quoted differently for cmd and PowerShell.
//
// An owner is given by chown -h with the uid's number, root for uid:0, or by icacls /setowner
// with the SID, so a caller's own Fix gives a name to this account with Account's answer as its
// Owner. That step's Admin is true: on Unix only root gives a name away, and on
// Windows an account sets itself as the owner only where the name's list grants it the right to
// take ownership. chown reads a number as a user name first where an account is so named, as
// POSIX says.
//
// Each step but the mode's acts on the name itself, never on what a link there names: the walk
// refuses a link by its own owner and, on Windows, its own list (rule 5), so its fix is for the
// link. Every icacls step passes /L, which takes the link itself, and the owner is set by icacls
// too, since takeown has no such switch; chown passes -h. chmod follows a link, as Linux keeps no
// mode of a link's own to change: trust gives a link no fix of mode bits, since the walk judges a
// link by its owner and lists alone, so only a caller's own Fix of a link with a mode reaches what
// the link names. The owner step comes last, so each step before it
// is made while the name's owner is still the one whose Admin false says may make it.
func (f Fix) Commands() (steps []Step, ok bool) {
	uid, byUID := strings.CutPrefix(f.Owner, "uid:")
	win := f.Reset || sidForm.MatchString(f.Owner) || f.Revoke != "" && sidForm.MatchString(f.Revoke)
	unix := f.Clear|f.Set != 0 || byUID || f.Revoke != "" && !win
	p := f.Path
	switch {
	case win == unix, strings.ContainsRune(p, 0):
		return nil, false
	case win && !winAbs.MatchString(p), unix && !strings.HasPrefix(p, "/"):
		return nil, false
	case f.Owner != "" && !ownerForm.MatchString(f.Owner):
		return nil, false
	case (f.Clear|f.Set)&^fs.ModePerm != 0:
		return nil, false
	case f.Revoke != "" && !sidForm.MatchString(f.Revoke):
		return nil, false // a macOS or NFSv4 entry takes more than a name to remove
	}
	if f.Reset {
		steps = append(steps, Step{Argv: []string{"icacls", p, "/reset", "/L"}})
	}
	if f.Clear|f.Set != 0 {
		mode := strings.TrimSuffix(symbolic(f.Clear, '-')+symbolic(f.Set, '+'), ",")
		steps = append(steps, Step{Argv: []string{"chmod", mode, p}})
	}
	if f.Revoke != "" {
		steps = append(steps,
			Step{Argv: []string{"icacls", p, "/inheritance:d", "/L"}},
			Step{Argv: []string{"icacls", p, "/remove:g", "*" + f.Revoke, "/L"}})
	}
	switch {
	case uid == "0":
		steps = append(steps, Step{Argv: []string{"chown", "-h", "root", p}, Admin: true})
	case byUID:
		steps = append(steps, Step{Argv: []string{"chown", "-h", uid, p}, Admin: true})
	case f.Owner != "":
		steps = append(steps, Step{Argv: []string{"icacls", p, "/setowner", "*" + f.Owner, "/L"}, Admin: true})
	}
	return steps, true
}

// symbolic is the bits of mode as chmod's symbolic words with op, "go-w" for 0o022 and '-': the
// classes that take the same letters said together, in u, g, o order, each followed by a comma.
func symbolic(mode fs.FileMode, op byte) string {
	var b strings.Builder
	who, letters := "", ""
	flush := func() {
		if letters != "" {
			b.WriteString(who + string(op) + letters + ",")
		}
	}
	for i, class := range "ugo" {
		var l []byte
		for j, c := range []byte("rwx") {
			if mode>>(3*(2-i))&(0o4>>j) != 0 {
				l = append(l, c)
			}
		}
		if string(l) != letters {
			flush()
			who, letters = "", string(l)
		}
		who += string(class)
	}
	flush()
	return b.String()
}

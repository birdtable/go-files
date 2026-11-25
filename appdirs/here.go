package appdirs

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
)

// thisKind is the layout of the system appdirs is built for. On each system one here_*.go file
// gives each part, and at most one part is not noKind, which is 0, so the sum is that one. The
// parts are ints, so no switch on a kind counts them among its cases.
const thisKind = kind(windowsPart + darwinPart + xdgPart)

// HomeVar is the environment variable HereWith reads the home from on the system appdirs is built
// for, as os.UserHomeDir reads it there: "USERPROFILE" on Windows, "HOME" on every Unix system,
// and "" on a system For names no folders for, where HereWith answers ErrSystem. The here_*.go
// files give its parts by their build constraints, as they give thisKind's, and at most one is not
// "". A test builds the environment it hands HereWith from map[string]string{appdirs.HomeVar: dir},
// so it names no system.
const HomeVar = windowsHome + darwinHome + xdgHome

// Here is For on the system it runs on, for the account that runs it: the system's layout,
// Windows', macOS's or the XDG one of the other Unix systems, is the one it is built for, which the
// here_*.go files give by their build constraints, the home is os.UserHomeDir, and the environment
// os.Getenv, each read once, at the call. A home the system will not say is ErrHome, with its
// cause, after the layout and the name. It answers as For does, ErrRun among the rest, and
// ErrSystem on a system For names no folders for, ios, js, plan9, wasip1 and hurd among them. For
// stays the call a test aims at another system or a home of its own, and HereWith the call of a
// caller that holds its own environment.
func Here(app string, layout Layout) (Folders, error) {
	if err := named(app, layout); err != nil {
		return Folders{}, err
	}
	home, herr := os.UserHomeDir()
	if herr != nil {
		home = ""
	}
	if thisKind == noKind {
		return Folders{}, fmt.Errorf("the system Here is built for: %w", ErrSystem)
	}
	f, err := folders(thisKind, app, home, os.Getenv, layout)
	if herr != nil && errors.Is(err, ErrHome) {
		return Folders{}, fmt.Errorf("%w: %w", err, herr)
	}
	return f, err
}

// HereWith is Here with the caller's environment: For on the system the here_*.go files build it
// for, with the home read from env by HomeVar, USERPROFILE on Windows and HOME on every Unix
// system, as os.UserHomeDir reads it. A nil env reads every variable as unset. An empty
// home, or one For refuses, is ErrHome: it never reads the process's own home or environment. It
// answers exactly as For does, ErrRun among the rest, after the layout and the name, and ErrSystem
// on a system For names no folders for: the call of a program that may not name the system.
func HereWith(app string, layout Layout, env func(string) string) (Folders, error) {
	return hereWith(thisKind, HomeVar, app, layout, env)
}

// Sources is, for each of the five folders For gives on the system appdirs is built for, in
// layout, the environment variable For reads that folder from there, or "" where no variable moves
// that folder on its own: XDG_CONFIG_HOME, XDG_STATE_HOME, XDG_CACHE_HOME, XDG_RUNTIME_DIR and
// XDG_DATA_HOME in the XDG layout, LOCALAPPDATA for all five on Windows, and "" for all five on
// macOS in Native. The home moves each folder whose variable is unset, but XDG's Run, "" without
// XDG_RUNTIME_DIR: its variable is HomeVar, no field repeats it, and a "" field moves with the home
// alone. A caller that may not name the system reads here which variables to set. A layout neither
// XDG nor Native is ErrLayout, and a system For names no folders for ErrSystem, as For refuses them.
func Sources(layout Layout) (Folders, error) {
	return sources(thisKind, layout)
}

// sources is Sources on a system of kind k.
func sources(k kind, layout Layout) (Folders, error) {
	if layout != XDG && layout != Native {
		return Folders{}, fmt.Errorf("layout %d: %w", layout, ErrLayout)
	}
	switch {
	case k == noKind:
		return Folders{}, fmt.Errorf("the system Sources is built for: %w", ErrSystem)
	case k == windowsKind:
		const v = "LOCALAPPDATA"
		return Folders{Config: v, State: v, Cache: v, Run: v, Data: v}, nil
	case k == darwinKind && layout == Native:
		return Folders{}, nil
	}
	return Folders{Config: "XDG_CONFIG_HOME", State: "XDG_STATE_HOME", Cache: "XDG_CACHE_HOME", Run: "XDG_RUNTIME_DIR", Data: "XDG_DATA_HOME"}, nil
}

// hereWith is HereWith on a system of kind k, whose home is the variable key.
func hereWith(k kind, key, app string, layout Layout, env func(string) string) (Folders, error) {
	if err := named(app, layout); err != nil {
		return Folders{}, err
	}
	if k == noKind {
		return Folders{}, fmt.Errorf("the system HereWith is built for: %w", ErrSystem)
	}
	home := ""
	if env != nil {
		home = env(key)
	}
	return folders(k, app, home, env, layout)
}

// System is the machine-wide folder of the program app on the system goos: the settings every
// account on the machine shares, which an administrator writes and the program only reads. On
// Windows it is %ProgramData%\<app>, read from env, or C:\ProgramData\<app> where env gives no
// absolute path on a drive; on macOS /Library/Application Support/<app>; on Linux, FreeBSD and
// the other Unix systems /etc/xdg/<app>, the default of XDG_CONFIG_DIRS, which is never read: a
// variable any account sets does not move the folder every account trusts. A nil env reads every
// variable as unset. The name is refused as For refuses it, with ErrApp, and a system For names
// no folders for with ErrSystem. Nothing is made, and nothing on the disk is read.
//
// Any account may make a folder in Windows' ProgramData, so a folder there the program's
// installer did not make may be another account's: a program judges it, with trust, before it
// reads settings from it.
//
// Windows' variable is read, where XDG_CONFIG_DIRS is not, because ProgramData may be moved and
// the variable is where the system says so, and because a folder on a drive it names is judged
// with trust as one in ProgramData must be anyway. A share (\\host\share) is never taken, and is
// C:\ProgramData instead: opening it offers that host this account's credentials before trust
// judges anything.
func System(goos, app string, env func(string) string) (string, error) {
	if !plain(app) {
		return "", fmt.Errorf("%q: %w", app, ErrApp)
	}
	switch kindOf(goos) {
	case windowsKind:
		base := ""
		if env != nil {
			base = env("PROGRAMDATA")
		}
		if !windowsAbs(base) || strings.HasPrefix(strings.ReplaceAll(base, "/", `\`), `\\`) {
			base = `C:\ProgramData`
		}
		return windowsJoin(base, app), nil
	case darwinKind:
		return path.Join("/Library/Application Support", app), nil
	case xdgKind:
		return path.Join("/etc/xdg", app), nil
	case noKind:
	}
	return "", fmt.Errorf("%q: %w", goos, ErrSystem)
}

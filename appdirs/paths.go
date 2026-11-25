// Package appdirs names the folders a program keeps its files in, on each system, and says how
// much room a folder has. For reads no environment and no disk: it is handed the system, the
// program's name, the home folder and a reader of the environment. The README has each layout.
//
// Built on the XDG Base Directory layout, macOS's Library and Windows' LOCALAPPDATA, as
// github.com/adrg/xdg reads them. os.UserConfigDir agrees on the XDG layout alone. On Windows it is
// the roaming %AppData%, which appdirs never uses. It adds the system, home and environment handed
// in, a Run and a Data folder, Here, System, Temp and Free. The program configures its name, the system, the home
// folder, the environment reader, the Layout on macOS, and Temp's apart folders.
// It imports the standard library, golang.org/x/sys, trust, confine and internal/fileid, and on Windows internal/winapi.
package appdirs

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/birdtable/go-files/confine"
)

const opFree = "free"

// Folders are the five folders of one program. None is made: the program makes the one it writes
// to, with the mode it wants.
type Folders struct {
	Config string // what a person edits to set the program up
	State  string // what one run leaves for the next: logs, locks, history
	Cache  string // what may be deleted at any time and made again; never Config or Data
	Run    string // sockets and process files, for while the program runs
	Data   string // what the program keeps that a person did not write
}

// Layout is the layout For gives a program's folders on macOS, XDG or Native. For takes it with no
// default, so each program chooses; any other Layout, the zero one among them, is ErrLayout on
// every system.
type Layout int

const (
	// XDG is the XDG Base Directory layout on macOS, as on Linux.
	XDG Layout = iota + 1
	// Native is macOS's own folders, under ~/Library: Caches/<app> for Cache, and Application
	// Support/<app> for the other four. It changes nothing on another system.
	Native
)

var (
	// ErrHome is For's refusal of a home folder that is empty or not an absolute path for the
	// system.
	ErrHome = errors.New("the home folder is not an absolute path")
	// ErrApp is For's refusal of a program name that is not one plain folder name on every system:
	// one holding "..", or one confine.Portable refuses, as an empty name, a separator, a colon, a
	// control character or one of <>"|?*, an end in a dot or a space, a Windows device (CON, NUL,
	// COM1, with or without an extension), an 8.3 short name or .git.
	ErrApp = errors.New("the program's name is not one plain folder name")
	// ErrSystem is For's refusal of a system it names no folders for.
	ErrSystem = errors.New("appdirs names no folders for this system")
	// ErrLayout is For's refusal of a Layout that is neither XDG nor Native, on every system.
	ErrLayout = errors.New("the layout is neither XDG nor Native")
	// ErrRun is For's answer in the XDG layout where XDG_RUNTIME_DIR is unset: Run is "" and the
	// other four folders are given. Run is never put in State, whose files outlive the session.
	ErrRun = errors.New("XDG_RUNTIME_DIR is unset, so there is no Run folder")
	// ErrNoAccessList is Temp's refusal where Windows would not make its folder this account's alone:
	// a volume that keeps no access lists (FAT, exFAT), one that will not say whether it does, a share
	// refusing this account as owner, or a folder where this account cannot open by name what it makes
	// there with that list, as under a restricted token or an AppContainer the list does not name, or
	// on a share that knows this account by another SID, which a throwaway folder made and removed
	// first finds (the cause follows the text, and a denial matches fs.ErrPermission too). Nothing is
	// made. It matches errors.ErrUnsupported.
	ErrNoAccessList = fmt.Errorf("the volume keeps no access lists, so a folder made here would not be this account's alone, and nothing was made: %w", errors.ErrUnsupported)
)

// For is the folders of the program app on the system goos, for the account whose home folder is
// home, with env reading that account's environment, in layout on macOS; a nil env reads every
// variable as unset. Linux, FreeBSD, the other Unix systems and macOS in XDG take the XDG layout,
// with Cache a folder deeper, as Cache, until it neither is nor holds Config or Data. There, where
// XDG_RUNTIME_DIR is unset, Run is "" and For gives the other four folders with an error matching
// ErrRun: a caller checks the error before it uses Run, and may use the other four past ErrRun.
// macOS in Native takes ~/Library. Windows takes LOCALAPPDATA, never the roaming folder, and joins
// with a backslash on every system. A variable whose value is not an absolute path is read as
// unset. A layout neither XDG nor Native is ErrLayout on every system.
func For(goos, app, home string, env func(string) string, layout Layout) (Folders, error) {
	if err := named(app, layout); err != nil {
		return Folders{}, err
	}
	k := kindOf(goos)
	if k == noKind {
		return Folders{}, fmt.Errorf("%q: %w", goos, ErrSystem)
	}
	return folders(k, app, home, env, layout)
}

// kind is how a system lays out a program's folders: Windows', macOS's, or the XDG layout of the
// other Unix systems.
type kind int

const (
	noKind kind = iota // a system For names no folders for
	windowsKind
	darwinKind
	xdgKind
)

// kindOf is the kind of the system goos, as GOOS names it.
func kindOf(goos string) kind {
	switch goos {
	case "windows":
		return windowsKind
	case "darwin":
		return darwinKind
	case "linux", "android", "freebsd", "openbsd", "netbsd", "dragonfly", "illumos", "solaris", "aix":
		return xdgKind
	}
	return noKind
}

// named refuses a layout neither XDG nor Native, with ErrLayout, then a name that is not one plain
// name, with ErrApp.
func named(app string, layout Layout) error {
	if layout != XDG && layout != Native {
		return fmt.Errorf("layout %d: %w", layout, ErrLayout)
	}
	if !plain(app) {
		return fmt.Errorf("%q: %w", app, ErrApp)
	}
	return nil
}

// folders is For past its checks of the layout, the name and the system, on a system of kind k.
func folders(k kind, app, home string, env func(string) string, layout Layout) (Folders, error) {
	abs := path.IsAbs
	if k == windowsKind {
		abs = windowsAbs
	}
	if !abs(home) {
		return Folders{}, fmt.Errorf("%q: %w", home, ErrHome)
	}
	get := func(key string) string {
		if env == nil {
			return ""
		}
		if v := env(key); abs(v) {
			return v
		}
		return ""
	}
	switch {
	case k == windowsKind:
		base := get("LOCALAPPDATA")
		if base == "" {
			base = windowsJoin(home, "AppData", "Local")
		}
		dir := windowsJoin(base, app)
		return Folders{Config: dir, State: windowsJoin(dir, "State"), Cache: windowsJoin(dir, "Cache"), Run: windowsJoin(dir, "Run"), Data: dir}, nil
	case k == darwinKind && layout == Native:
		support := path.Join(home, "Library", "Application Support", app)
		return Folders{Config: support, State: support, Cache: path.Join(home, "Library", "Caches", app), Run: support, Data: support}, nil
	}
	in := func(key string, def ...string) string {
		if v := get(key); v != "" {
			return path.Join(v, app)
		}
		return path.Join(append(append([]string{home}, def...), app)...)
	}
	f := Folders{Config: in("XDG_CONFIG_HOME", ".config"), State: in("XDG_STATE_HOME", ".local", "state"), Cache: in("XDG_CACHE_HOME", ".cache"), Data: in("XDG_DATA_HOME", ".local", "share")}
	for holds(f.Cache, f.Config) || holds(f.Cache, f.Data) {
		f.Cache = path.Join(f.Cache, "Cache")
	}
	v := get("XDG_RUNTIME_DIR")
	if v == "" {
		return f, ErrRun
	}
	f.Run = path.Join(v, app)
	return f, nil
}

func holds(dir, p string) bool {
	return p == dir || strings.HasPrefix(p, strings.TrimSuffix(dir, "/")+"/")
}

func windowsAbs(p string) bool {
	p = strings.ReplaceAll(p, "/", `\`)
	var rest string
	switch {
	case len(p) >= 3 && p[1] == ':' && p[2] == '\\' && ('a' <= p[0]|0x20 && p[0]|0x20 <= 'z'):
		rest = p[3:]
	case strings.HasPrefix(p, `\\`):
		host, share, _ := strings.Cut(p[2:], `\`)
		share, rest, _ = strings.Cut(share, `\`)
		if host == "" || host == "?" || host == "." || share == "" {
			return false
		}
	default:
		return false
	}
	for e := range strings.SplitSeq(rest, `\`) {
		if e == "." || e == ".." {
			return false
		}
	}
	return true
}

func windowsJoin(base string, elems ...string) string {
	return strings.Join(append([]string{strings.TrimRight(strings.ReplaceAll(base, "/", `\`), `\`)}, elems...), `\`)
}

// plain is whether app is one plain folder name on every system: confine.Portable passes it, and
// it holds no "..", which Portable passes inside a name.
func plain(app string) bool {
	_, err := confine.Portable(app)
	return err == nil && !strings.Contains(app, "..")
}

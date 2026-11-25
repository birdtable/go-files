//go:build windows

package testtoken

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Plain disables SeBackupPrivilege and SeRestorePrivilege in the process's own token, by
// AdjustTokenPrivileges with no attribute, so the tests run with neither enabled, as a program's
// process does. It is for a hosted runner, where a read the access list denies and
// a make with an owner no token holds both passed: the second is what SeRestorePrivilege allows,
// as internal/winapi's test of it says, and the first is read as SeBackupPrivilege's. A privilege
// the token does not hold, or holds and has not enabled, is left as it is, and a token that has
// neither enabled is not opened to be changed. The error is a token that could not be read, a
// privilege the token holds enabled that could not be disabled, or one still enabled once it was.
func Plain() error {
	want, err := two()
	if err != nil {
		return err
	}
	_, _, err = plain(want, disable)
	return err
}

// privilege is one privilege: the name it goes by, and the number this system knows it by.
type privilege struct {
	name string
	luid windows.LUID
}

// state is what a token says of one privilege.
type state int

const (
	absent  state = iota // the token does not hold it
	off                  // the token holds it, and it is not enabled
	enabled              // the token holds it enabled
)

// String says a state as the test log prints it.
func (s state) String() string {
	switch s {
	case off:
		return "held and not enabled"
	case enabled:
		return "enabled"
	case absent:
	}
	return "not held"
}

// two is the two privileges Plain disables, each by the number this system knows it by.
func two() ([]privilege, error) {
	var out []privilege
	for _, name := range []string{"SeBackupPrivilege", "SeRestorePrivilege"} {
		u, err := windows.UTF16PtrFromString(name)
		if err != nil {
			return nil, fmt.Errorf("testtoken: %s: %w", name, err)
		}
		p := privilege{name: name}
		if err := windows.LookupPrivilegeValue(nil, u, &p.luid); err != nil {
			return nil, fmt.Errorf("testtoken: asking this system for %s: %w", name, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// plain reads what the process's token says of each of want, has disable disable each it holds
// enabled, and reads it again where it had any disabled; before and after are the two reads, in
// want's order. It calls disable for no other privilege, and reads a token it changed nothing of
// once. The error is a read's, disable's, or a privilege still enabled after disable answered nil.
func plain(want []privilege, disable func(privilege) error) (before, after []state, err error) {
	before, err = read(want)
	if err != nil {
		return nil, nil, err
	}
	changed := false
	for i, p := range want {
		if before[i] != enabled {
			continue
		}
		if err := disable(p); err != nil {
			return before, nil, err
		}
		changed = true
	}
	if !changed {
		return before, before, nil
	}
	after, err = read(want)
	if err != nil {
		return before, nil, err
	}
	for i, p := range want {
		if after[i] == enabled {
			return before, after, fmt.Errorf("testtoken: %s is still enabled in the process's token after it was disabled", p.name)
		}
	}
	return before, after, nil
}

// read is what the process's token says of each of want, in want's order.
func read(want []privilege) ([]state, error) {
	held, err := held()
	if err != nil {
		return nil, err
	}
	out := make([]state, len(want))
	for i, w := range want {
		for _, h := range held {
			switch {
			case h.Luid != w.luid:
			case h.Attributes&windows.SE_PRIVILEGE_ENABLED != 0:
				out[i] = enabled
			default:
				out[i] = off
			}
		}
	}
	return out, nil
}

// held is every privilege the process's token holds, with its attributes, read through a handle
// that may query the token and nothing more.
func held() (list []windows.LUIDAndAttributes, err error) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return nil, fmt.Errorf("testtoken: opening the process's token to read it: %w", err)
	}
	defer func() { err = errors.Join(err, token.Close()) }()
	size := uint32(1024)
	for {
		buf := make([]byte, size)
		var n uint32
		err = windows.GetTokenInformation(token, windows.TokenPrivileges, &buf[0], size, &n)
		if err == nil {
			all := (*windows.Tokenprivileges)(unsafe.Pointer(&buf[0])).AllPrivileges() //nolint:gosec // G103: buf is the TOKEN_PRIVILEGES GetTokenInformation filled, read as one and copied before buf goes
			return append([]windows.LUIDAndAttributes(nil), all...), nil
		}
		// The system says how much the list needs; a second refusal at that size is the error.
		if !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) || n <= size {
			return nil, fmt.Errorf("testtoken: reading the privileges of the process's token: %w", err)
		}
		size = n
	}
}

// disable disables p in the process's own token, through a handle opened for the one change.
func disable(p privilege) (err error) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY|windows.TOKEN_ADJUST_PRIVILEGES, &token); err != nil {
		return fmt.Errorf("testtoken: opening the process's token to disable %s: %w", p.name, err)
	}
	defer func() { err = errors.Join(err, token.Close()) }()
	to := windows.Tokenprivileges{PrivilegeCount: 1, Privileges: [1]windows.LUIDAndAttributes{{Luid: p.luid, Attributes: 0}}}
	if err := windows.AdjustTokenPrivileges(token, false, &to, 0, nil, nil); err != nil {
		return fmt.Errorf("testtoken: disabling %s in the process's token: %w", p.name, err)
	}
	return nil
}

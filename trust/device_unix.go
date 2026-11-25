//go:build unix && !linux

package trust

import "os"

// onOtherDevice is whether entry, read with lstat, is on another device than parent, read with
// stat: how macOS and every Unix but Linux tell a mount's root.
func onOtherDevice(entry, parent string) (bool, error) {
	e, err := os.Lstat(entry)
	if err != nil {
		return false, err
	}
	p, err := os.Stat(parent)
	if err != nil {
		return false, err
	}
	en, err := nodeOf(e, entry)
	if err != nil {
		return false, err
	}
	pn, err := nodeOf(p, parent)
	return err == nil && en.id.vol != pn.id.vol, err
}

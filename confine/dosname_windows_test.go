package confine

import (
	"errors"
	"io/fs"
	"testing"
)

// Each of dosName's refusals matches fs.ErrInvalid, a failed look at the letter among them, and
// keeps the look's own error; a letter that names a device and a mounted volume passes.
//
// What it costs when this fails: a caller that refuses by errors.Is(err, fs.ErrInvalid) takes a
// failed QueryDosDevice for some other failure, and may try the link again.
func TestEachDriveRefusalMatchesErrInvalid(t *testing.T) {
	t.Parallel()
	failed := errors.New("QueryDosDevice failed")
	unmounted := errors.New("no volume is mounted there")
	device := func(dev string, err error) func(string) (string, error) {
		return func(string) (string, error) { return dev, err }
	}
	mounted := func(string) error { return nil }
	notMounted := func(string) error { return unmounted }
	for _, c := range []struct {
		name   string
		device func(string) (string, error)
		volume func(string) error
		cause  error
	}{
		{"the look fails", device("", failed), mounted, failed},
		{"subst", device(`\??\C:\folder`, nil), mounted, nil},
		{"a raw target", device(`\Device\HarddiskVolume3\folder`, nil), notMounted, unmounted},
	} {
		err := dosName("X:", c.device, c.volume)
		if !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("%s: %v, want an error matching fs.ErrInvalid", c.name, err)
		}
		if c.cause != nil && !errors.Is(err, c.cause) {
			t.Errorf("%s: %v, want it to keep %v", c.name, err, c.cause)
		}
	}
	if err := dosName("X:", device(`\Device\HarddiskVolume3`, nil), mounted); err != nil {
		t.Errorf("a device with a mounted volume: %v, want nil", err)
	}
}

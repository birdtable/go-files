package trust

import (
	"errors"
	"testing"
)

// What it costs when this fails: a program that lets the admin group write its folders lets
// another group in, gid 0 or a number cut from one the lookup misspoke, or takes a failed lookup
// as a group, and an account outside the admin group changes what the program runs.
func TestTheAdminGroupIsTheLookupsGidAsADecimal32BitNumberOrAnError(t *testing.T) {
	t.Parallel()
	answer := func(gid string, err error) func(string) (string, error) {
		return func(name string) (string, error) {
			if name != "admin" {
				t.Errorf("looked up %q, and want admin", name)
			}
			return gid, err
		}
	}
	gid, ok, err := adminOf("admin", answer("80", nil))
	if err != nil || !ok || gid != 80 {
		t.Errorf("gid \"80\": %d, %t, %v, and want 80, true, nil", gid, ok, err)
	}
	if gid, ok, err := adminOf("admin", answer("4294967295", nil)); err != nil || !ok || gid != 4294967295 {
		t.Errorf("the largest gid: %d, %t, %v, and want it whole", gid, ok, err)
	}
	unknown := errors.New("group: unknown group admin")
	if gid, ok, err := adminOf("admin", answer("80", unknown)); !errors.Is(err, unknown) || ok || gid != 0 {
		t.Errorf("a lookup that fails: %d, %t, %v, and want 0, false and its error", gid, ok, err)
	}
	for _, s := range []string{"", "-1", "+80", "0x50", "80 ", " 80", "8O", "4294967296", "18446744073709551696", "80.0"} {
		if gid, ok, err := adminOf("admin", answer(s, nil)); err == nil || ok || gid != 0 {
			t.Errorf("gid %q: %d, %t, %v, and want 0, false and an error", s, gid, ok, err)
		}
	}
}

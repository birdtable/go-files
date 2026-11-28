//go:build windows

package trust

import (
	"testing"

	"golang.org/x/sys/windows"
)

// trust may not import internal/winapi, so its one CreateFile passes identify, its own copy of
// winapi.Identify, the pair whose level internal/winapi's tests read natively: a pipe's server
// opened by it takes on the caller's token at SecurityIdentification alone. The lint pins the call
// to pass identify; this pins identify to that pair.
//
// What it costs when this fails: an open trust makes of a name before it has judged the folders on
// the way lets a pipe a junction leads to act as the program, where the hardening says its server
// may only learn who the program is.
func TestTrustsOpensAskForTheSameIdentificationAloneAsWinapisDo(t *testing.T) {
	t.Parallel()
	if want := uint32(windows.SECURITY_SQOS_PRESENT | windows.SECURITY_IDENTIFICATION); identify != want {
		t.Errorf("trust's identify is %#x, and want SECURITY_SQOS_PRESENT with SECURITY_IDENTIFICATION, %#x", identify, want)
	}
}

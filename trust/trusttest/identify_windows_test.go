//go:build windows

package trusttest

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// trusttest may not import internal/winapi, so its one CreateFile passes identify, its own copy of
// winapi.Identify, and its reopen gives NtCreateFile identifyOnly, its own copy of the quality of
// service OpenAt gives: the pair and the level internal/winapi's tests read natively, at which a
// pipe's server takes on the caller's token at SecurityIdentification alone. The lint pins the
// calls to pass them; this pins them to that pair and that level.
//
// What it costs when this fails: a test helper's open of a name a junction leads to a pipe lets
// the pipe's server act as the test's account.
func TestTrusttestsOpensAskForTheSameIdentificationAloneAsWinapisDo(t *testing.T) {
	t.Parallel()
	if want := uint32(windows.SECURITY_SQOS_PRESENT | windows.SECURITY_IDENTIFICATION); identify != want {
		t.Errorf("trusttest's identify is %#x, and want SECURITY_SQOS_PRESENT with SECURITY_IDENTIFICATION, %#x", identify, want)
	}
	q := identifyOnly()
	want := windows.SECURITY_QUALITY_OF_SERVICE{
		Length:              uint32(unsafe.Sizeof(windows.SECURITY_QUALITY_OF_SERVICE{})),
		ImpersonationLevel:  windows.SecurityIdentification,
		ContextTrackingMode: windows.SECURITY_STATIC_TRACKING,
	}
	if q == nil || *q != want {
		t.Errorf("trusttest's identifyOnly is %+v, and want identification with static tracking, %+v", q, want)
	}
}

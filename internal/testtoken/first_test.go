package testtoken

import (
	"errors"
	"fmt"
	"testing"
)

// What it costs when this fails: a token whose privileges could not be disabled runs the tests
// anyway, and the two refusals a plain token is for pass on it with nothing said; or a token made plain
// stops the run, or is not recorded, so Held fails every package's tests.
func TestATokenThatCannotBeMadePlainEndsTheRunBeforeAnyTest(t *testing.T) {
	t.Parallel()
	refused := errors.New("refused")
	calls := 0
	if code, done, err := first(func() error { calls++; return refused }); code != 1 || !done || !errors.Is(err, refused) || made.Load() {
		t.Errorf("first with a drop that fails: %d, %v, %v, made %v, and want 1, done, the drop's error and not made", code, done, err, made.Load())
	}
	if code, done, err := first(func() error { calls++; return nil }); code != 0 || done || err != nil || !made.Load() {
		t.Errorf("first with a drop that answers nil: %d, %v, %v, made %v, and want 0, not done, no error and made", code, done, err, made.Load())
	}
	if calls != 2 {
		t.Errorf("drop was called %d times over two runs of first, and want once each", calls)
	}
}

// fatals is a testing.TB that records what Fatal said in place of ending the test.
type fatals struct {
	testing.TB

	said []string
}

func (f *fatals) Helper()           {}
func (f *fatals) Fatal(args ...any) { f.said = append(f.said, fmt.Sprint(args...)) }

// What it costs when this fails: a package whose TestMain does not run First runs its tests on the
// token the host gave, and its guard passes.
func TestHeldFailsATestOnATokenFirstDidNotMakePlain(t *testing.T) {
	t.Parallel()
	for _, ok := range []bool{false, true} {
		f := &fatals{TB: t}
		madeOr(f, ok)
		if (len(f.said) == 1) == ok || len(f.said) > 1 {
			t.Errorf("madeOr with made %v said %q, and want one failure only when not made", ok, f.said)
		}
	}
}

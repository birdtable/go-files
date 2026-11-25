package testtoken

import (
	"fmt"
	"sync/atomic"
	"testing"
)

// made is whether First made the token plain in this process. First writes it in TestMain before
// any test runs, and Held reads it.
var made atomic.Bool //nolint:gochecknoglobals // First writes it once, before the tests, and Held reads it; a test binary's TestMain has nowhere else to say what it did

// First is the f of the testkit.Before a package's TestMain gives testkit.Main, so it runs before
// the process is isolated and before any test: it makes the test process's token plain (Plain), so
// every process a test starts takes the token as it is left. When it cannot, it ends the run with
// code 1 and Plain's error before any test has run, since a test run on a token with either
// privilege enabled may pass a refusal that holds for every program; nothing is
// skipped. It reads nothing of the environment the process started in.
func First(_ []string) (code int, done bool, err error) {
	return first(Plain)
}

// first is First with drop in place of Plain.
func first(drop func() error) (code int, done bool, err error) {
	if err := drop(); err != nil {
		return 1, true, fmt.Errorf("the tests could not disable the backup and restore privileges in their token, so none ran: %w", err)
	}
	made.Store(true)
	return 0, false, nil
}

// Held fails t unless First made the token plain in this process before the tests ran.
func Held(t testing.TB) {
	t.Helper()
	madeOr(t, made.Load())
}

// madeOr fails t unless ok, what made says.
func madeOr(t testing.TB, ok bool) {
	t.Helper()
	if !ok {
		t.Fatal("the tests started on a token First did not make plain: TestMain gives testkit.Main testkit.Before(testtoken.First)")
	}
}

package write

import (
	"os"
	"testing"

	"github.com/birdtable/go-files/internal/testtoken"
	"github.com/birdtable/go-testkit"
)

// TestMain runs this binary as a helper process (helper) when it is started so, and otherwise
// starts the tests on a plain token (testtoken.First) from the environment testkit.Main makes:
// no variable of the person's or an agent's, and the home, temp and XDG_ folders beneath one
// removed after the run. An environment it cannot make, a token it cannot make plain, or one it
// cannot put back ends the run red.
func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == helperArg {
		os.Exit(helper(os.Args[2], os.Args[3:]))
	}
	code, err := testkit.Main(m, testkit.Before(testtoken.First))
	if err != nil {
		panic(err)
	}
	os.Exit(code)
}

// What it costs when this fails: a test reads or writes the person's own home, temp folder or
// settings, or runs with an agent's or git's variables, or on a token with the backup and restore
// privileges enabled, and passes or fails on what the machine holds.
func TestTheTestsRunInAnEnvironmentOfTheirOwnOnAPlainToken(t *testing.T) {
	t.Parallel()
	testkit.Isolated(t)
	testtoken.Held(t)
}

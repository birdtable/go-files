package filestest_test

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/birdtable/go-files/filestest"
	"github.com/birdtable/go-files/internal/testtoken"
	"github.com/birdtable/go-testkit"
)

// The arguments that start this test binary as a helper process instead of its tests.
const (
	privateArg = "filestest-private" // Private, a number of times, each path on a line
	exitArg    = "filestest-exit"    // the program WriteProgram wrote on Windows: it exits 0
)

// TestMain runs this binary as a helper process when it is started so, and otherwise starts the
// tests on a plain token (testtoken.First) from the environment testkit.Main makes: no variable of
// the person's or an agent's, and the home, temp and XDG_ folders beneath one removed after the
// run. An environment it cannot make, a token it cannot make plain, or one it cannot put back ends
// the run red.
func TestMain(m *testing.M) {
	switch {
	case len(os.Args) > 1 && os.Args[1] == exitArg:
		os.Exit(0)
	case len(os.Args) > 2 && os.Args[1] == privateArg:
		os.Exit(privateHelper(os.Args[2]))
	}
	code, err := testkit.Main(m, testkit.Before(testtoken.First))
	if err != nil {
		panic(err)
	}
	os.Exit(code)
}

// privateHelper takes count folders from Private, says each path on a line of its stdout, and
// removes them as a test's end does. It is the helper process's exit code.
func privateHelper(count string) int {
	n, err := strconv.Atoi(count)
	if err != nil {
		fmt.Fprintln(os.Stderr, err) //nolint:forbidigo // the helper process says why it failed on its stderr, which the test reads
		return 1
	}
	r := (&recorder{}).run(func(r *recorder) {
		for range n {
			fmt.Fprintln(os.Stdout, filestest.Private(r)) //nolint:forbidigo // the helper says each path on its stdout, the pipe the test reads
		}
	})
	r.end()
	if r.failed {
		fmt.Fprintln(os.Stderr, r.said) //nolint:forbidigo // the helper process says why it failed on its stderr, which the test reads
		return 1
	}
	return 0
}

// What it costs when this fails: a test reads or writes the person's own home, temp folder or
// settings, or runs with an agent's or git's variables, or on a token with the backup and restore
// privileges enabled, and passes or fails on what the machine holds.
func TestTheTestsRunInAnEnvironmentOfTheirOwnOnAPlainToken(t *testing.T) {
	t.Parallel()
	testkit.Isolated(t)
	testtoken.Held(t)
}

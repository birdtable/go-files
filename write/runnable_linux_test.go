package write

import (
	"fmt"
	"os/exec" //nolint:depguard // cli.process.1: the test forks in a loop and runs the files it wrote, the fork and exec race this guards; the module has no runner
	"path/filepath"
	"sync"
	"testing"
)

// Eight goroutines each write a Runnable file of their own and run it, 300 times, so each one's
// run is a fork that lands while the others write; every run starts. The writes take NoSync, so
// nothing between a file's close and its run gives a stray child time to exec. Run it through the
// slot with -race.
//
// What it costs when this fails: a child forked between the temp file's create and its close
// holds it open for writing, and the run fails with text file busy (ETXTBSY) until that child
// execs or exits, the fork and exec race of golang/go#22315.
func TestOnLinuxARunnableFileRunsWhileTheProcessForks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var writers sync.WaitGroup
	for w := range 8 {
		writers.Go(func() {
			path := filepath.Join(dir, fmt.Sprintf("prog%d", w))
			for round := range 300 {
				if err := File(path, []byte(prog), Runnable(), NoSync()); err != nil {
					t.Error(err)
					return
				}
				if err := ranIt(exec.CommandContext(t.Context(), path).Run()); err != nil { //nolint:gosec // the file this test wrote
					t.Errorf("writer %d, round %d: %v; text file busy is a fork that holds the file open for writing", w, round, err)
					return
				}
			}
		})
	}
	writers.Wait()
}

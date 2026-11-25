package gofiles_test

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"maps"
	"math"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test runs the secrets task, and git to lay out its trees, as a person runs them; the module has no runner
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// noHistory is the start of the line the secrets task says when it fails for want of history.
const noHistory = "secrets: no history to scan: "

// TestSecretsFailsWhereTheTreeHasNoHistoryOfItsOwn runs the secrets task, as Taskfile.yml has
// it, in copies of this tree: one with no .git (as a tree taken from a tarball), one inside a
// folder that is a repository with a commit of its own, one in a repository with no commit, and
// one in a repository with the tree committed. gitleaks git passes the first three having
// scanned no commit of the tree's own; the task must fail them, saying why, and pass the last.
// It runs silent, so a line in what it says is the task's own and not a command it echoes.
func TestSecretsFailsWhereTheTreeHasNoHistoryOfItsOwn(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		// make lays the tree out under root, and says the folder the task runs in.
		make func(t *testing.T, root string) string
		pass bool
	}{
		{"a tree with no .git", func(t *testing.T, root string) string {
			t.Helper()
			copyTree(t, root)
			return root
		}, false},
		{"a tree inside a folder that is a repository with a commit", func(t *testing.T, root string) string {
			t.Helper()
			git(t, root, exec.CommandContext(t.Context(), "git", "init", "-q"))
			git(t, root, exec.CommandContext(t.Context(), "git", "-c", "user.name=test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "the folder's"))
			tree := filepath.Join(root, "tree")
			copyTree(t, tree)
			return tree
		}, false},
		{"a tree in a repository with no commit", func(t *testing.T, root string) string {
			t.Helper()
			copyTree(t, root)
			git(t, root, exec.CommandContext(t.Context(), "git", "init", "-q"))
			return root
		}, false},
		{"a tree committed in a repository of its own", func(t *testing.T, root string) string {
			t.Helper()
			copyTree(t, root)
			git(t, root, exec.CommandContext(t.Context(), "git", "init", "-q"))
			git(t, root, exec.CommandContext(t.Context(), "git", "add", "-A"))
			git(t, root, exec.CommandContext(t.Context(), "git", "-c", "user.name=test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "the tree"))
			return root
		}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := c.make(t, t.TempDir())
			cmd := taskCmd(t, "--silent", "secrets")
			cmd.Dir = dir
			isolate(cmd)
			out, err := cmd.CombinedOutput()
			said := saysNoHistory(out)
			switch {
			case c.pass && (err != nil || said):
				t.Fatalf("%s: task secrets: %v, and want it to pass:\n%s", c.name, err, out)
			case !c.pass && (err == nil || !said):
				t.Fatalf("%s: task secrets: %v, and want it to fail with %q:\n%s", c.name, err, noHistory, out)
			}
		})
	}
}

// upToDate is the line go-task says when it skips fmt.
const upToDate = `task: Task "fmt" is up to date`

// TestAMemberKilledBeforeItsStampRunsAgain runs fmt, as Taskfile.yml has it, in a module that holds
// this tree's Taskfile and one package (taskModule): a second run skips it, as its inputs are what
// they were at its pass. go-task writes a
// member's checksum before the member runs, so a run killed mid-member leaves the checksum and no
// stamp; the test leaves the tree so, the checksum kept and the stamp removed, and fmt must run.
// The stamp is named by the Go version, so a stamp another Go wrote is no stamp: fmt, its stamp
// renamed to an older Go's, runs again.
func TestAMemberKilledBeforeItsStampRunsAgain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	taskModule(t, dir, map[string]string{"doc.go": "// Package planted is fmt's to read.\npackage planted\n"})
	v, err := exec.CommandContext(t.Context(), "go", "env", "GOVERSION").Output()
	if err != nil {
		t.Fatalf("go env GOVERSION, which names the stamp: %v", err)
	}
	stamp := filepath.Join(dir, ".task", "pass", "fmt-"+strings.TrimSpace(string(v)))
	run := func() string {
		t.Helper()
		cmd := taskCmd(t, "fmt")
		cmd.Dir = dir
		isolate(cmd)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("task fmt: %v:\n%s", err, out)
		}
		return string(out)
	}
	if out := run(); strings.Contains(out, upToDate) {
		t.Fatalf("the first run of fmt skipped it:\n%s", out)
	}
	if out := run(); !strings.Contains(out, upToDate) {
		t.Fatalf("the second run of fmt, its inputs unchanged, ran it:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".task", "checksum", "fmt")); err != nil {
		t.Fatalf("fmt's checksum, which a killed run leaves: %v", err)
	}
	if err := os.Remove(stamp); err != nil {
		t.Fatalf("removing fmt's stamp: %v", err)
	}
	if out := run(); strings.Contains(out, upToDate) {
		t.Fatalf("fmt, its checksum kept and its stamp gone, as a killed run leaves it, skipped:\n%s", out)
	}
	if err := os.Rename(stamp, filepath.Join(dir, ".task", "pass", "fmt-go1.0")); err != nil {
		t.Fatalf("renaming fmt's stamp to an older Go's: %v", err)
	}
	if out := run(); strings.Contains(out, upToDate) {
		t.Fatalf("fmt, its stamp one an older Go wrote, skipped:\n%s", out)
	}
}

// TestVetAndBuildFailWhereOneSystemFails runs vet and build, as Taskfile.yml has them, each in a
// module that holds this tree's Taskfile, go.mod, go.sum and task's modfile, and one package planted
// with a fault one system alone reads: a Printf vet refuses, on darwin/amd64, and a line that does
// not compile, on windows/amd64. Each member runs its systems together, vet's eight and build's
// six, and waits for every one, so the one fault fails it, whichever system it is on
// and in whatever order they end. The module holds no other package, so the runs read the planted
// one alone.
func TestVetAndBuildFailWhereOneSystemFails(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		member, file, body string
		// task is the member's run, its words spelled here.
		task func(t *testing.T) *exec.Cmd
	}{
		{"vet", "planted_darwin_amd64.go", "package planted\n\nimport \"fmt\"\n\n// F prints.\nfunc F() { fmt.Printf(\"%d\\n\", \"x\") }\n", func(t *testing.T) *exec.Cmd {
			t.Helper()
			return taskCmd(t, "vet")
		}},
		{"build", "planted_windows_amd64.go", "package planted\n\nvar v int = \"x\"\n", func(t *testing.T) *exec.Cmd {
			t.Helper()
			return taskCmd(t, "build")
		}},
	} {
		t.Run(c.member, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			taskModule(t, dir, map[string]string{"doc.go": "// Package planted holds a fault one system reads.\npackage planted\n", c.file: c.body})
			cmd := c.task(t)
			cmd.Dir = dir
			isolate(cmd)
			out, err := cmd.CombinedOutput()
			if err == nil || !bytes.Contains(out, []byte(c.file)) {
				t.Fatalf("task %s with a fault in planted/%s: %v, and want it to fail naming the file:\n%s", c.member, c.file, err, out)
			}
		})
	}
}

// taskModule makes, in dir, a module go-task runs a member in: this tree's Taskfile, go.mod, go.sum
// and task's modfile, and the package planted, which holds files, each name's body. A member then
// reads planted alone, not the whole tree, so its run is short.
func taskModule(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for _, d := range []string{"tools", "planted"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"Taskfile.yml", "go.mod", "go.sum", filepath.Join("tools", "task.mod"), filepath.Join("tools", "task.sum")} {
		if err := copyFile(p, filepath.Join(dir, p)); err != nil {
			t.Fatalf("copying %s: %v", p, err)
		}
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, "planted", name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// copyTree copies this tree, every file but .git's and go-task's .task's (the checksums and stamps
// of this clone's runs), to dst.
func copyTree(t *testing.T, dst string) {
	t.Helper()
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.Name() == ".git" || d.Name() == ".task":
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		case d.IsDir():
			return os.MkdirAll(filepath.Join(dst, p), 0o700)
		case !d.Type().IsRegular():
			return nil
		}
		return copyFile(p, filepath.Join(dst, p))
	})
	if err != nil {
		t.Fatalf("copying the tree to %s: %v", dst, err)
	}
}

// copyFile copies the plain file src to dst, which it makes.
func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { err = joinClose(err, in) }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { err = joinClose(err, out) }()
	_, err = io.Copy(out, in)
	return err
}

// joinClose is err, or the error closing c when err is nil.
func joinClose(err error, c io.Closer) error {
	if cerr := c.Close(); err == nil {
		return cerr
	}
	return err
}

// taskProgram is the path of the task program tools/task.mod pins, as go tool -n names it once it
// has built it. Each run of go tool has the go command load task's packages before it starts task,
// most of a second each, and the tests here run task some forty times at once. The
// test binary asks once, in the first test to want it, and every test runs the program itself: an
// ask of each test's own cost two seconds of CPU on a Mac, some ten at once, the most of any
// step of a push's root package. What go tool names is held to one absolute path of
// a plain file, and a failed ask fails every test that wants the program.
func taskProgram(t *testing.T) string {
	t.Helper()
	asked.once.Do(func() { asked.path, asked.err = askTask(t) })
	if asked.err != nil {
		t.Fatal(asked.err)
	}
	return asked.path
}

// taskCmd is a run of the task program with words, and the one place a test here builds one. It
// starts task with --color=false: Task colours what it writes wherever CI is set, with a terminal
// or none, as a hosted runner sets it, and the tests here read whole lines. The flag
// outranks CI, FORCE_COLOR and NO_COLOR, so a test reads the same line on every host. No test
// strips a colour code from what it reads. The caller sets the run's folder, and then isolates it.
func taskCmd(t *testing.T, words ...string) *exec.Cmd {
	t.Helper()
	return exec.CommandContext(t.Context(), taskProgram(t), append([]string{"--color=false"}, words...)...) //nolint:gosec // G204: the task program go tool named, and the words a test spells out; no shell reads them
}

// TestATaskATestStartsWritesNoColourWhateverTheEnvironmentSays starts task as every test here
// does, through taskCmd, with CI=true and FORCE_COLOR=1 in its environment, as a hosted runner
// sets the first, and reads what a dry run of warm writes: it must hold each command's line and
// no escape. Then it reads this package's test files and holds every use of taskProgram to
// taskCmd's one, so no test builds a run of task the flag is not on.
//
// What it costs when this fails: on a host that sets CI every line task writes starts with a colour
// code, and each test that reads a whole line is red there and green on a Mac, as the hosted
// release run was from 18 Nov; or a test starts task its own way, and is the one
// the flag misses.
func TestATaskATestStartsWritesNoColourWhateverTheEnvironmentSays(t *testing.T) {
	t.Parallel()
	cmd := taskCmd(t, "--dry", "warm")
	isolate(cmd)
	cmd.Env = append(cmd.Env, "CI=true", "FORCE_COLOR=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("task --dry warm: %v:\n%s", err, out)
	}
	if !saysLine(out, regexp.MustCompile(`^task: \[warm\] `)) {
		t.Fatalf("task --dry warm said no line of a command, so the test read nothing:\n%q", out)
	}
	if i := bytes.IndexByte(out, 0x1b); i >= 0 {
		t.Errorf("task --dry warm, started with CI=true and FORCE_COLOR=1, wrote an escape at byte %d of what it said:\n%q", i, out)
	}
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	uses := 0
	for _, name := range files {
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				fn = &ast.FuncDecl{Name: ast.NewIdent("a declaration outside any function")}
			}
			ast.Inspect(d, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Name == "taskProgram" && id != fn.Name {
					uses++
					if fn.Name.Name != "taskCmd" {
						t.Errorf("%s: %s names taskProgram, and only taskCmd may: a run of task built anywhere else is started without --color=false", name, fn.Name.Name)
					}
				}
				return true
			})
		}
	}
	if len(files) < 2 || uses != 1 {
		t.Fatalf("read %d test files and found %d uses of taskProgram, and want taskCmd's one", len(files), uses)
	}
}

// asked is what taskProgram's one ask found: the tests' own, set once, and no library state.
var asked struct { //nolint:gochecknoglobals // the test binary asks go tool for task's path once, for every test that runs it
	once sync.Once
	path string
	err  error
}

// askTask asks go tool -n for the task program's path, in the context of the test that asks first,
// which waits in taskProgram until the ask is done.
func askTask(t *testing.T) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", "tool", "-modfile=tools/task.mod", "-n", "task")
	isolate(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go tool -n task: %w: %s", err, stderr.String())
	}
	p := strings.TrimSuffix(strings.TrimSuffix(string(out), "\n"), "\r")
	fi, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("go tool -n task named %q, and want the absolute path of one plain file: %w", out, err)
	}
	if !filepath.IsAbs(p) || strings.ContainsAny(p, "\r\n") || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("go tool -n task named %q, and want the absolute path of one plain file", out)
	}
	return p, nil
}

// git runs cmd, a git command the caller spells out, in dir, isolated, and fails the test if it
// fails.
func git(t *testing.T, dir string, cmd *exec.Cmd) {
	t.Helper()
	cmd.Dir = dir
	isolate(cmd)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s in %s: %v:\n%s", strings.Join(cmd.Args, " "), dir, err, out.String())
	}
}

// TestNoTaskStateIsTracked fails when git tracks a file under .task/, in the index or in a commit.
// go-task keeps there the checksum and the stamp that let a member skip, and a skip exits 0, so a
// tracked .task/ comes with every checkout and reports a check that never ran on it as passed.
// .gitignore leaves .task/ out, so only a forced add brings one in. Where git reads no repository
// here, a tree from the module cache or a tarball, nothing is tracked, and it passes.
//
// What it costs when this fails: a pull request that forces a .task/ in turns lint, vuln and the
// other members that skip into passes for code they never read, wherever a member is not forced.
func TestNoTaskStateIsTracked(t *testing.T) {
	t.Parallel()
	repo := exec.CommandContext(t.Context(), "git", "rev-parse", "--git-dir")
	isolate(repo)
	if out, err := repo.CombinedOutput(); err != nil {
		if exit := (*exec.ExitError)(nil); errors.As(err, &exit) {
			t.Logf("git reads no repository here, so it tracks nothing: %s", bytes.TrimSpace(out))
			return
		}
		t.Fatalf("git rev-parse --git-dir: %v", err)
	}
	ls := exec.CommandContext(t.Context(), "git", "ls-files", "--", ".task")
	isolate(ls)
	out, err := ls.Output()
	if err != nil {
		t.Fatalf("git ls-files -- .task: %v", err)
	}
	if len(bytes.TrimSpace(out)) > 0 {
		t.Fatalf("git tracks go-task's state under .task/, which only a run may write: a stamp in a checkout makes a member skip and pass unrun. Take it out of the index with git rm --cached:\n%s", out)
	}
}

// isolate gives cmd the environment it would have, as exec.Cmd.Environ says it, less every GIT_
// variable, which a hook running the tests may set to this clone's repository, and with no
// global or system git config read.
func isolate(cmd *exec.Cmd) {
	var env []string
	for _, kv := range cmd.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
}

// saysNoHistory says whether a line of out starts with noHistory: the task's own line, not the
// script that says it.
func saysNoHistory(out []byte) bool {
	for line := range strings.Lines(string(out)) {
		if strings.HasPrefix(line, noHistory) {
			return true
		}
	}
	return false
}

// testBudget is the seconds the test's budgets file gives each tier, far over what a run of budget
// takes, so a tier started now is within it at any load.
const testBudget = 300

// offline is the environment a run of hatch gets in these tests: no module proxy, so hatch runs from
// what `go mod download` left in the module cache, as a tier must.
const offline = "GOPROXY=off"

// budgetDir makes a folder budget runs in: gate/budget.sh as the tree has it, a gate/budgets.yaml
// that names this host with testBudget for each tier's wall budget and pre-push's CPU limit, or
// with the rows files gives it at gate/budgets.yaml, go.mod and hatch's and go-task's modfiles,
// which go tool reads their pins from (pre-push's step runs its members by task), and the files
// named in files, relative to it.
func budgetDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	// budget reads the host's kind from uname -sm, its space a dash, so the test's file names it so.
	kind, err := exec.CommandContext(t.Context(), "uname", "-sm").Output()
	if err != nil {
		t.Fatalf("uname -sm, which budget reads the host's kind from: %v", err)
	}
	host := strings.ReplaceAll(strings.TrimSpace(string(kind)), " ", "-")
	rows := fmt.Sprintf("pre-commit-budget: %d\npre-push-budget: %d\npre-push-cpu-limit: %d\nrelease-budget: %d\n", testBudget, testBudget, testBudget, testBudget)
	if given, ok := files["gate/budgets.yaml"]; ok {
		rows = given
	}
	files["gate/budgets.yaml"] = "host: " + host + "\n" + rows
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "tools"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join("gate", "budget.sh"), "go.mod", filepath.Join("tools", "hatch.mod"), filepath.Join("tools", "hatch.sum"), filepath.Join("tools", "task.mod"), filepath.Join("tools", "task.sum")} {
		if err := copyFile(p, filepath.Join(dir, p)); err != nil {
			t.Fatalf("copying %s: %v", p, err)
		}
	}
	return dir
}

// endTaskfile writes the start and load the environment's STATE gives to the file a tier's start
// step writes, unless NOSTATE is set, and runs budget's end step for the tier the environment's T
// names, in one run of go-task, so both have its pid: the test's way in, as a tier names its own in
// literal words.
const endTaskfile = `version: "3"
tasks:
  end:
    silent: true
    cmds:
      - if [ -z "$NOSTATE" ]; then mkdir -p .task/budget && printf '%s\n' "$STATE" > ".task/budget/$T.$$"; fi
      - tier="$T"; step=end; . ./gate/budget.sh
`

// TestBudgetFailsALateTierButACommitStartedAboveLoad15 runs budget's end step, as gate/budget.sh has
// it, on the start and load a tier's start step wrote, against a budgets file that names this host,
// with hatch budget offline: a tier within its budget passes; past it, a release fails at any load,
// and a commit fails unless the load at its start was above 15, where it passes with the line that
// says so. pre-push, which is judged by its CPU time, is refused at the end step, so no push is
// judged by its wall time alone. A tier, start or load that is not a plain name or number is
// refused, so no spelling a number parser reads (1e9, 0x20) passes for a load; so are a missing
// start, one after now, and a field more.
func TestBudgetFailsALateTierButACommitStartedAboveLoad15(t *testing.T) {
	t.Parallel()
	now := time.Now().Unix()
	late, inTime, ahead := strconv.FormatInt(now-10*testBudget, 10), strconv.FormatInt(now, 10), strconv.FormatInt(now+10*testBudget, 10)
	past := func(tier string) string {
		return fmt.Sprintf(`^hatch budget: %s is over its budget: a tier over its budget is a speed bug`, tier)
	}
	within := func(tier string) string {
		return fmt.Sprintf(`^budget: %s took \d+ s of %d s$`, tier, testBudget)
	}
	pd58 := func(tier, load string) string {
		return fmt.Sprintf(`^budget: %s is over budget, passed at the load it started at: load %s before %s, above the limit of 15$`, tier, load, tier)
	}
	const none = "\x00" // a row whose tier's start step wrote no file
	for _, c := range []struct {
		name, tier, state string // state: what the tier's start step wrote, or none
		pass              bool
		line              string // a line of what budget says, as a regular expression
	}{
		{"a commit past its budget at load 16 passes, saying so", "pre-commit", late + " 16", true, pd58("pre-commit", "16")},
		{"a commit past its budget at load 15.01 passes, saying so", "pre-commit", late + " 15.01", true, pd58("pre-commit", "15")},
		{"a commit past its budget at load 15 fails", "pre-commit", late + " 15", false, past("pre-commit")},
		{"a commit past its budget with no load read fails", "pre-commit", late + " ", false, past("pre-commit")},
		{"a release past its budget at load 16 fails", "release", late + " 16", false, past("release")},
		{"a release past its budget at load 90 fails", "release", late + " 90", false, past("release")},
		{"a commit within its budget passes", "pre-commit", inTime + " ", true, within("pre-commit")},
		{"a commit within its budget at load 16 passes", "pre-commit", inTime + " 16", true, within("pre-commit")},
		{"a release within its budget passes", "release", inTime + " ", true, within("release")},
		{"a load read at the start is handed to hatch", "pre-commit", inTime + " 16.25", true, handedLoadLine},
		{"a load a number parser reads as 1e9 is refused", "pre-commit", late + " 1e9", false, `gives the load as '1e9', and wants`},
		{"a load a number parser reads as 0x20 is refused", "pre-commit", late + " 0x20", false, `gives the load as '0x20', and wants`},
		{"a load of two dots is refused", "pre-commit", late + " 16.0.1", false, `gives the load as '16.0.1', and wants`},
		{"a start that is not whole seconds is refused", "pre-commit", "1; 16", false, `gives the start as '1;', and wants`},
		{"a start with a leading 0 is refused", "pre-commit", "0" + late + " 16", false, `gives the start as '0\d+', and wants`},
		{"a field after the load is refused", "pre-commit", late + " 16 0", false, `has '0' after the start and the load`},
		{"a start after now is refused", "pre-commit", ahead + " 16", false, `^budget: pre-commit started at \d+, after now, so its time would be -\d+s$`},
		{"a tier with no start is refused", "pre-commit", none, false, `^budget: \.task/budget/pre-commit\.\d+ is missing, so pre-commit has no start to time from$`},
		{"a tier budget does not hold is refused", "pre-pushy", late + " 16", false, `^budget: the tier is 'pre-pushy', and wants`},
		{"a push at the end step is refused", "pre-push", inTime + " 16", false, `^budget: pre-push has no step 'end': pre-push takes cpu, and pre-commit and release take start and end$`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := budgetDir(t, map[string]string{"Taskfile.yml": endTaskfile})
			cmd := taskCmd(t, "-d", dir, "end")
			isolate(cmd)
			cmd.Env = append(cmd.Env, "T="+c.tier, offline)
			if c.state == none {
				cmd.Env = append(cmd.Env, "NOSTATE=1")
			} else {
				cmd.Env = append(cmd.Env, "STATE="+c.state)
			}
			out, err := cmd.CombinedOutput()
			switch {
			case c.pass && err != nil:
				t.Fatalf("budget: %v, and want it to pass:\n%s", err, out)
			case !c.pass && err == nil:
				t.Fatalf("budget passed, and want it to fail:\n%s", out)
			case !c.pass && bytes.Contains(out, []byte("passed at the load it started at")):
				t.Fatalf("budget failed with the line that says a tier passed:\n%s", out)
			}
			if !saysLine(out, regexp.MustCompile(c.line)) {
				t.Fatalf("budget said no line matching %s:\n%s", c.line, out)
			}
			// A tier the end step holds has its file read once and removed; one it does not is
			// refused before any path is made of its name, so its file is left as it was.
			if c.tier != "pre-commit" && c.tier != "release" {
				return
			}
			if left := startsLeft(t, dir); len(left) != 0 {
				t.Fatalf("budget left %v behind in .task/budget, and want its file read once and removed", left)
			}
		})
	}
}

// startsLeft is the files in dir's .task/budget, where a tier's start step writes.
func startsLeft(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(dir, ".task", "budget"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

// members is each tier's members task in Taskfile.yml, its deps line to be put by a command that
// does nothing. pre-push's is not internal, so that the run of task its step hands hatch can name it.
var members = regexp.MustCompile(`(?m)^(  (?:pre-commit|pre-push|release)-members:\n(?:    internal: true\n)?)    deps: \[[^\]\n]*\]$`)

// stubTiers is Taskfile.yml with each tier's members put by the one command cmd, a line of YAML.
func stubTiers(t *testing.T, cmd string) string {
	t.Helper()
	b, err := os.ReadFile("Taskfile.yml")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(members.FindAllIndex(b, -1)); n != 3 {
		t.Fatalf("Taskfile.yml has %d members tasks of the shape the test stubs, and want 3: pre-commit-members, pre-push-members and release-members", n)
	}
	return string(members.ReplaceAll(b, []byte("${1}    cmds: ["+strings.ReplaceAll(cmd, "$", "$$")+"]")))
}

// TestATierTimesItselfWhateverTheEnvironmentSays runs each tier of Taskfile.yml, its members put by
// a command that does nothing, with LOAD=16, START=1, another tier's TIER, and the script's own
// tier and step exported, as `LOAD=16 git push` hands them to the hook. None of these may reach
// budget: the tier is timed from its own start, at the load it read,
// under its own name.
func TestATierTimesItselfWhateverTheEnvironmentSays(t *testing.T) {
	t.Parallel()
	warmPushTools(t)
	stubbed := stubTiers(t, `"true"`)
	for _, c := range []struct{ tier, other string }{
		{"pre-commit", "release"},
		{"pre-push", "release"},
		{"release", "pre-push"},
	} {
		t.Run(c.tier, func(t *testing.T) {
			t.Parallel()
			dir := budgetDir(t, map[string]string{"Taskfile.yml": stubbed})
			cmd := taskCmd(t, "-d", dir, c.tier)
			isolate(cmd)
			cmd.Env = append(cmd.Env, "LOAD=16", "START=1", "TIER="+c.other, "tier="+c.other, "step=end", offline)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("task %s: %v, and want it to pass within its budget:\n%s", c.tier, err, out)
			}
			line := fmt.Sprintf(`^budget: %s took \d+ s of %d s$`, c.tier, testBudget)
			if !saysLine(out, regexp.MustCompile(line)) {
				t.Fatalf("task %s said no line matching %s, so a start or tier came from the environment:\n%s", c.tier, line, out)
			}
			// pre-push is judged by its CPU time, under its own name.
			if cpu := fmt.Sprintf(`^budget: pre-push used \d+\.\d s of CPU of %d s$`, testBudget); c.tier == "pre-push" && !saysLine(out, regexp.MustCompile(cpu)) {
				t.Fatalf("task pre-push said no line matching %s:\n%s", cpu, out)
			}
			// A load hatch read from the host is said with one decimal, and 16.0 only by chance;
			// where the host has none, hatch says it is unknown.
			if saysLine(out, regexp.MustCompile(`^budget: load 16\.0 before `)) {
				t.Fatalf("task %s said the load before it was 16.0, as the environment did:\n%s", c.tier, out)
			}
		})
	}
}

// TestTwoRunsOfATierInOneTreeEachTimeThemselves runs pre-commit, as Taskfile.yml has it, twice at
// once in one tree, its members put by a command that says it started and then reads a line. The
// test reads each run's word before it lets either go on, so both start steps are done before
// either end step, and lets the second go on only once the first has ended. Each run must time
// itself from its own start and pass: a start kept under the tier's name alone is written twice
// and read once, and the second run to end finds none. pre-push keeps no start: its one step holds
// it, and runs its members.
func TestTwoRunsOfATierInOneTreeEachTimeThemselves(t *testing.T) {
	t.Parallel()
	dir := budgetDir(t, map[string]string{"Taskfile.yml": stubTiers(t, `"echo started; read -r line"`)})
	type run struct {
		cmd        *exec.Cmd
		in         io.WriteCloser
		out        *bufio.Reader
		said, errs bytes.Buffer
	}
	var runs [2]*run
	for i := range runs {
		r := &run{cmd: taskCmd(t, "-d", dir, "pre-commit")}
		isolate(r.cmd)
		r.cmd.Env = append(r.cmd.Env, offline)
		r.cmd.Stderr = &r.errs
		in, err := r.cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		out, err := r.cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		r.in, r.out = in, bufio.NewReader(out)
		if err := r.cmd.Start(); err != nil {
			t.Fatal(err)
		}
		runs[i] = r
	}
	// Each run's members say started only after its start step wrote its start; a run that ends
	// first says something else, and its end of output is a failure here.
	for _, r := range runs {
		for {
			line, err := r.out.ReadString('\n')
			r.said.WriteString(line)
			if strings.TrimSpace(line) == "started" {
				break
			}
			if err != nil {
				for _, r := range runs {
					_ = r.in.Close()
				}
				t.Fatalf("a run of pre-commit ended before its members started: %v\n%s%s", err, r.said.String(), r.errs.String())
			}
		}
	}
	// One run is let go and ends, end step and all, before the other is let go: two end steps at
	// once could each read a shared start before either removed it.
	for _, r := range runs {
		if _, err := io.WriteString(r.in, "go on\n"); err != nil {
			t.Fatal(err)
		}
		if err := r.in.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(&r.said, r.out); err != nil {
			t.Fatal(err)
		}
		// Wait is done copying the run's stderr when it returns, so errs is read after it.
		err := r.cmd.Wait()
		out := append(r.said.Bytes(), r.errs.Bytes()...)
		if err != nil {
			t.Errorf("one of two runs of pre-commit at once: %v, and want each to pass on its own start:\n%s", err, out)
			continue
		}
		if line := fmt.Sprintf(`^budget: pre-commit took \d+ s of %d s$`, testBudget); !saysLine(out, regexp.MustCompile(line)) {
			t.Errorf("one of two runs of pre-commit at once said no line matching %s:\n%s", line, out)
		}
	}
	if left := startsLeft(t, dir); len(left) != 0 {
		t.Fatalf("two runs of pre-commit left %v in .task/budget, and want each run's start read and removed", left)
	}
}

// pushTools is hatch and go-task built once as pre-push's step runs them: with cgo off, as
// Taskfile.yml's env has every cmd, where taskProgram's task is built with cgo on. A run of
// pre-push whose step builds either counts that build in its CPU time, and on a cold build cache,
// with tests building it at once, runs past a hang guard of 10 s.
var pushTools struct { //nolint:gochecknoglobals // the test binary builds pre-push's tools once, for every test that runs pre-push
	once sync.Once
	err  error
}

// warmPushTools builds pushTools once, as Taskfile.yml's warm does, and fails the test where a
// build fails.
func warmPushTools(t *testing.T) {
	t.Helper()
	pushTools.once.Do(func() {
		for _, tool := range []string{"hatch", "task"} {
			cmd := exec.CommandContext(t.Context(), "go", "tool", "-modfile=tools/"+tool+".mod", "-n", tool) //nolint:gosec // G204: go tool and a name spelled out here; no shell reads them
			isolate(cmd)
			cmd.Env = append(cmd.Env, "CGO_ENABLED=0")
			if out, err := cmd.CombinedOutput(); err != nil {
				pushTools.err = fmt.Errorf("go tool -n %s with cgo off: %w: %s", tool, err, out)
				return
			}
		}
	})
	if pushTools.err != nil {
		t.Fatal(pushTools.err)
	}
}

// pushTier runs pre-push, as Taskfile.yml has it, with hatch budget offline, in a folder whose
// gate/budgets.yaml names this host with the rows given, its members put by the one command cmd.
// A run of pre-push leaves no start behind: its one step keeps none in a file.
func pushTier(t *testing.T, rows, cmd string) ([]byte, error) {
	t.Helper()
	warmPushTools(t)
	dir := budgetDir(t, map[string]string{"Taskfile.yml": stubTiers(t, cmd), "gate/budgets.yaml": rows})
	run := taskCmd(t, "-d", dir, "pre-push")
	isolate(run)
	run.Env = append(run.Env, offline)
	out, err := run.CombinedOutput()
	if left := startsLeft(t, dir); len(left) != 0 {
		t.Fatalf("pre-push left %v in .task/budget, and want no start kept in a file", left)
	}
	return out, err
}

// TestThePushTierIsJudgedByItsCPUTime runs pre-push, as Taskfile.yml has it, under hatch budget
// --cpu, its wall budget far over what a run takes: members within the CPU limit pass, saying the
// CPU time they used; over it they fail, at a wall time within budget; members that fail fail the
// tier; and a budgets file with no CPU row for pre-push refuses it, so a push is never judged by its
// wall time alone.
//
// What it costs when this fails: a push whose tier used more CPU than its limit passes, and the tier
// grows slower unseen; or a tier whose CPU row is gone falls back to wall time, which a loaded
// machine fails.
func TestThePushTierIsJudgedByItsCPUTime(t *testing.T) {
	t.Parallel()
	wall := fmt.Sprintf("pre-push-budget: %d\n", testBudget)
	for _, c := range []struct {
		name, rows, cmd string
		pass            bool
		line            string // a line of what the tier says, as a regular expression
	}{
		{"members within the CPU limit pass", wall + fmt.Sprintf("pre-push-cpu-limit: %d\n", testBudget), `"true"`, true, fmt.Sprintf(`^budget: pre-push used \d+\.\d s of CPU of %d s$`, testBudget)},
		{"members over the CPU limit fail", wall + "pre-push-cpu-limit: 0\n", `"true"`, false, `^hatch budget: pre-push is over its CPU budget$`},
		{"members that fail fail the tier", wall + fmt.Sprintf("pre-push-cpu-limit: %d\n", testBudget), `"false"`, false, `^hatch budget: pre-push: the command exited [1-9]\d*$`},
		{"a tier with no CPU row is refused", wall, `"true"`, false, `has no single CPU limit for pre-push, and a tier is never judged by wall time alone under --cpu$`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, err := pushTier(t, c.rows, c.cmd)
			switch {
			case c.pass && err != nil:
				t.Fatalf("task pre-push: %v, and want it to pass:\n%s", err, out)
			case !c.pass && err == nil:
				t.Fatalf("task pre-push passed, and want it to fail:\n%s", out)
			}
			if !saysLine(out, regexp.MustCompile(c.line)) {
				t.Fatalf("task pre-push said no line matching %s:\n%s", c.line, out)
			}
		})
	}
}

// TestThePushTiersWallTimeIsOnlyAHangGuard runs pre-push, as Taskfile.yml has it, under hatch budget
// --cpu with a wall budget of 1 s: members that sleep 2 s pass on their CPU time, saying the tier was
// over its wall limit; members that sleep 30 s are killed at 10 times the wall budget, and the tier
// fails as hung. It runs over a second and guards no fail-open, so pre-push leaves it to release
// (pushSkips).
//
// What it costs when this fails: a loaded machine fails a push whose CPU time is within its limit;
// or a hung tier holds a push open with nothing to end it.
func TestThePushTiersWallTimeIsOnlyAHangGuard(t *testing.T) {
	t.Parallel()
	rows := fmt.Sprintf("pre-push-budget: 1\npre-push-cpu-limit: %d\n", testBudget)
	for _, c := range []struct {
		name, cmd string
		pass      bool
		line      string // a line of what the tier says, as a regular expression
	}{
		{"members over the wall budget pass on their CPU time", `"sleep 2"`, true, `^budget: pre-push is over its wall limit of 1 s, and passes or fails on its CPU time alone`},
		{"members still running at 10 times the wall budget fail as hung", `"sleep 30"`, false, `^hatch budget: pre-push: hung: still running at 10s, which is 10 times its wall limit, and its tree was killed$`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, err := pushTier(t, rows, c.cmd)
			switch {
			case c.pass && err != nil:
				t.Fatalf("task pre-push: %v, and want it to pass:\n%s", err, out)
			case !c.pass && err == nil:
				t.Fatalf("task pre-push passed, and want it to fail:\n%s", out)
			}
			if !saysLine(out, regexp.MustCompile(c.line)) {
				t.Fatalf("task pre-push said no line matching %s:\n%s", c.line, out)
			}
		})
	}
}

// TestATestSkippedIsRed runs Taskfile.yml's test member, as pre-push calls it, on a planted module:
// one passing test is green; a test skipped, a test that fails, and a module with no test are each
// red, with hatch's words for why. The test member rests each red on its own word in --red-when
// (any-skipped, failed, none-ran), and a word left out never makes a run red (hatch's usage), so
// each has its row. A skip on one system is a test
// that system never ran, so no tier passes with one.
func TestATestSkippedIsRed(t *testing.T) {
	t.Parallel()
	taskfile, err := os.ReadFile("Taskfile.yml")
	if err != nil {
		t.Fatal(err)
	}
	const head = "package planted\n\nimport \"testing\"\n\n"
	const passes = head + "func TestPasses(t *testing.T) {}\n"
	for _, c := range []struct {
		name  string
		tests map[string]string // the module's test files, by name
		green bool
		line  string // hatch's verdict line, as a regular expression
	}{
		{"one passing test is green", map[string]string{"passes_test.go": passes}, true,
			`^hatch tests: 1 passed, 0 failed, 0 skipped: green$`},
		{"a test skipped is red", map[string]string{"passes_test.go": passes, "skips_test.go": head + "func TestSkips(t *testing.T) { t.Skip(\"planted\") }\n"}, false,
			`^hatch tests: 1 passed, 0 failed, 1 skipped: red, 1 skipped: example\.com/planted\.TestSkips$`},
		{"a test that fails is red", map[string]string{"passes_test.go": passes, "fails_test.go": head + "func TestFails(t *testing.T) { t.Fatal(\"planted\") }\n"}, false,
			`^hatch tests: 1 passed, 1 failed, 0 skipped: red, tests failed$`},
		{"a module with no test is red", map[string]string{"planted.go": "package planted\n"}, false,
			`^hatch tests: 0 passed, 0 failed, 0 skipped: red, no tests ran$`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			files := map[string]string{
				"Taskfile.yml": string(taskfile),
				"go.mod":       "module example.com/planted\n\ngo 1.27.1\n",
			}
			maps.Copy(files, c.tests)
			for name, body := range files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Mkdir(filepath.Join(dir, "tools"), 0o750); err != nil {
				t.Fatal(err)
			}
			for _, p := range []string{filepath.Join("tools", "hatch.mod"), filepath.Join("tools", "hatch.sum")} {
				if err := copyFile(p, filepath.Join(dir, p)); err != nil {
					t.Fatalf("copying %s: %v", p, err)
				}
			}
			cmd := taskCmd(t, "-d", dir, "test", "RACE_EXIT_SLEEP=none")
			isolate(cmd)
			cmd.Env = append(cmd.Env, offline)
			out, err := cmd.CombinedOutput()
			switch {
			case c.green && err != nil:
				t.Fatalf("task test: %v, and want it green:\n%s", err, out)
			case !c.green && err == nil:
				t.Fatalf("task test passed, and want it red:\n%s", out)
			case !saysLine(out, regexp.MustCompile(c.line)):
				t.Fatalf("task test said no verdict matching %s:\n%s", c.line, out)
			}
		})
	}
}

// TestTheBudgetFileHoldsEachTiersBudgetAndFloor reads gate/budgets.yaml: on the kind of host its
// host line names, a tier's budget is commit 2 s, push 10 s and release 15 s, and 60 s for release on
// Linux or Windows amd64 outside a hosted runner. Each tier's floor, the least time measured, is
// written beside its budget as a time above 0, and is not held to be under its budget. pre-push's
// CPU limit is twice its CPU floor, rounded up to whole seconds.
func TestTheBudgetFileHoldsEachTiersBudgetAndFloor(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("gate", "budgets.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for line := range strings.Lines(string(b)) {
		line = strings.TrimRight(line, "\r\n")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ": ")
		if !ok {
			t.Fatalf("gate/budgets.yaml: %q is no comment and no key, a colon, a space and a value, the shape hatch's row and budget's awk read", line)
		}
		got[k] = v
	}
	for k, want := range map[string]string{"host": "Darwin-arm64", "pre-commit-budget": "2", "pre-push-budget": "10", "release-budget": "15", "release-session-budget": "60"} {
		if got[k] != want {
			t.Errorf("gate/budgets.yaml: %s is %q, and want %q", k, got[k], want)
		}
	}
	for _, tier := range []string{"pre-commit", "pre-push", "release"} {
		floor, err := strconv.ParseFloat(got[tier+"-floor"], 64)
		if err != nil {
			t.Errorf("gate/budgets.yaml: %s-floor is %q, and wants the seconds measured: %v", tier, got[tier+"-floor"], err)
			continue
		}
		if floor <= 0 {
			t.Errorf("gate/budgets.yaml: %s-floor is %v, and wants a time above 0", tier, floor)
		}
	}
	// pre-push's CPU limit is twice its CPU floor, the least CPU time of three runs, rounded up to
	// the whole seconds hatch reads a limit in.
	cpuFloor, err := strconv.ParseFloat(got["pre-push-cpu-floor"], 64)
	if err != nil || cpuFloor <= 0 {
		t.Errorf("gate/budgets.yaml: pre-push-cpu-floor is %q, and wants the CPU seconds measured, above 0", got["pre-push-cpu-floor"])
	} else if want := strconv.Itoa(int(math.Ceil(2 * cpuFloor))); got["pre-push-cpu-limit"] != want {
		t.Errorf("gate/budgets.yaml: pre-push-cpu-limit is %q, and want %s, twice pre-push-cpu-floor rounded up to whole seconds", got["pre-push-cpu-limit"], want)
	}
}

// What it costs when this fails: task api goes back into pre-push and fills its 10 s budget,
// or leaves release too, and no run holds the exported surface before a tag.
func TestTheAPIGateRunsInReleaseAndNotInPrePush(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("Taskfile.yml")
	if err != nil {
		t.Fatal(err)
	}
	api := regexp.MustCompile(`[\[ ]api[,\]]`)
	found := map[string]bool{}
	for _, m := range members.FindAllSubmatch(b, -1) {
		tier := strings.TrimSuffix(strings.TrimSpace(strings.SplitN(string(m[1]), ":", 2)[0]), "-members")
		found[tier] = api.Match(m[0][len(m[1]):])
	}
	for tier, want := range map[string]bool{"pre-commit": false, "pre-push": false, "release": true} {
		if has, ok := found[tier]; !ok {
			t.Errorf("Taskfile.yml has no %s-members task of the shape members reads", tier)
		} else if has != want {
			t.Errorf("Taskfile.yml: api is a member of %s: %v, and want %v", tier, has, want)
		}
	}
}

// TestThePromiseIndexGateRunsInPreCommit reads Taskfile.yml: contract is a member of pre-commit,
// its command is go test -count=1 of ./internal/contract/, and it names no sources:, so no checksum
// of the tree skips it. pre-push and release run that package in test, with every other.
//
// What it costs when this fails: a commit lands a line of api/contract.txt whose test is gone or
// can skip on a system the line names, and no run says so before the push; or the commit tier
// skips the gate on a tree it passed before a new tag moved the base of its removed-line check.
func TestThePromiseIndexGateRunsInPreCommit(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("Taskfile.yml")
	if err != nil {
		t.Fatal(err)
	}
	contract := regexp.MustCompile(`[\[ ]contract[,\]]`)
	found := false
	for _, m := range members.FindAllSubmatch(b, -1) {
		if strings.HasPrefix(string(m[1]), "  pre-commit-members:") {
			found = contract.Match(m[0][len(m[1]):])
		}
	}
	if !found {
		t.Error("Taskfile.yml: contract is not a member of pre-commit")
	}
	block := taskBlock(t, string(b), "contract")
	if !strings.Contains(block, "      - cmd: CGO_ENABLED=0 go test -count=1 ./internal/contract/\n") {
		t.Errorf("Taskfile.yml: task contract does not run go test -count=1 ./internal/contract/:\n%s", block)
	}
	if strings.Contains(block, "sources:") {
		t.Errorf("Taskfile.yml: task contract names sources:, so a checksum skips it:\n%s", block)
	}
}

// TestTheAPIGateAtATaggedHeadHoldsTheBreaksSinceTheTagBefore runs task api in a scratch repository
// whose head is tagged v0.2.0 over v0.1.0 and removes an exported name v0.1.0 had: with no line in
// api/breaks/v0.1.0.txt it is red, and with apidiff's line there it is green.
//
// What it costs when this fails: the gate runs in the release tier alone, and a run at a tagged head
// takes that tag as its base, finds nothing between the tree and itself, and is green, so a break no
// earlier release-tier run saw reaches the tag unheld.
func TestTheAPIGateAtATaggedHeadHoldsTheBreaksSinceTheTagBefore(t *testing.T) {
	t.Parallel()
	const (
		doc  = "// Package planted holds the names the gate reads.\npackage planted\n"
		kept = "\n// Kept is in both tags.\nfunc Kept() {}\n"
		gone = "\n// Gone is in v0.1.0 alone.\nfunc Gone() {}\n"
		line = "- ./planted.Gone: removed\n"
	)
	for _, c := range []struct {
		name, breaks string
		green        bool
		says         string
	}{
		{"no line for the removed name is red", "", false, "api: the breaks since v0.1.0 are not api/breaks/v0.1.0.txt"},
		{"apidiff's line for it is green", line, true, "api: the breaks since v0.1.0 are the 1 lines of api/breaks/v0.1.0.txt"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			taskModule(t, dir, map[string]string{"doc.go": doc, "planted.go": "package planted\n" + kept + gone})
			for _, d := range []string{"gate", "api", filepath.Join("api", "breaks")} {
				if err := os.Mkdir(filepath.Join(dir, d), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			for _, p := range []string{filepath.Join("gate", "api.sh"), filepath.Join("tools", "apidiff.mod"), filepath.Join("tools", "apidiff.sum")} {
				if err := copyFile(p, filepath.Join(dir, p)); err != nil {
					t.Fatalf("copying %s: %v", p, err)
				}
			}
			write := func(p, body string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, p), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join("api", "systems.txt"), "")
			write(filepath.Join("api", "marks.txt"), "")
			commit := func(tag string) {
				t.Helper()
				git(t, dir, exec.CommandContext(t.Context(), "git", "add", "-A"))
				git(t, dir, exec.CommandContext(t.Context(), "git", "-c", "user.name=test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "-m", tag)) //nolint:gosec // G204: git, and the tag this test spells out as the message; no shell reads it

				git(t, dir, exec.CommandContext(t.Context(), "git", "tag", tag)) //nolint:gosec // G204: git, and a tag this test spells out; no shell reads it
			}
			git(t, dir, exec.CommandContext(t.Context(), "git", "init", "-q"))
			commit("v0.1.0")
			write(filepath.Join("planted", "planted.go"), "package planted\n"+kept)
			if c.breaks != "" {
				write(filepath.Join("api", "breaks", "v0.1.0.txt"), c.breaks)
			}
			commit("v0.2.0")
			cmd := taskCmd(t, "api")
			cmd.Dir = dir
			isolate(cmd)
			out, err := cmd.CombinedOutput()
			if (err == nil) != c.green || !bytes.Contains(out, []byte(c.says)) {
				t.Fatalf("task api at v0.2.0, Gone removed since v0.1.0, api/breaks/v0.1.0.txt %q: %v, and want green %v saying %q:\n%s", c.breaks, err, c.green, c.says, out)
			}
		})
	}
}

// saysLine says whether a line of out, its line end trimmed, matches re.
func saysLine(out []byte, re *regexp.Regexp) bool {
	for line := range strings.Lines(string(out)) {
		if re.MatchString(strings.TrimRight(line, "\r\n")) {
			return true
		}
	}
	return false
}

// TestOnlyThePushTierRunsTheTestsWithNoRaceExitSleep reads each tier's commands as go-task would
// run them (--dry runs nothing), with RACE_EXIT_SLEEP=none in the environment as a caller could set
// it. Pre-push exports GORACE with no exit sleep; release must not, so a race only a goroutine left
// running after its test makes is still reported there, on every system its workflow runs. Pre-push
// alone leaves tests to release by -skip, exactly pushSkips, by one -skip, and each
// test it leaves is one this tree holds, so a rename cannot drop a test from both tiers.
//
// What it costs when this fails: no tier sees a race in a goroutine that outlives its test, or in
// a helper process after its work, and a release ships one; or a test is left out of release too,
// or the push leaves out a test its list no longer names, or a test a guard row names.
func TestOnlyThePushTierRunsTheTestsWithNoRaceExitSleep(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		tier string
		none bool
	}{
		{"pre-push", true},
		{"release", false},
	} {
		t.Run(c.tier, func(t *testing.T) {
			t.Parallel()
			cmd := taskCmd(t, "--dry", dryTier(c.tier), "VERSION=v0.0.1")
			isolate(cmd)
			cmd.Env = append(cmd.Env, "RACE_EXIT_SLEEP=none")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("task --dry %s: %v\n%s", c.tier, err, out)
			}
			if !bytes.Contains(out, []byte("hatch tests")) {
				t.Fatalf("task --dry %s names no test run, and want its test member's commands:\n%s", c.tier, out)
			}
			if got := bytes.Contains(out, []byte("atexit_sleep_ms=0")); got != c.none {
				t.Fatalf("task --dry %s: the race exit sleep is none: %v, and want %v:\n%s", c.tier, got, c.none, out)
			}
			raw, err := os.ReadFile("Taskfile.yml")
			if err != nil {
				t.Fatal(err)
			}
			if err := noGoflags(string(raw)); err != nil {
				t.Fatalf("%v, and want no flag set outside the test member's command words", err)
			}
			left, err := leftOut(out, c.none)
			if err != nil {
				t.Fatalf("task --dry %s: %v, and want a test a guard row names to stay in the push tier: pre-push leaves %s to release by one -skip and no -run, release leaves none:\n%s", c.tier, err, pushSkips, out)
			}
			for _, name := range left {
				if !heldTest(t, name) {
					t.Errorf("task --dry %s leaves %s to release, and no test of this tree is named so", c.tier, name)
				}
			}
		})
	}
}

// dryTier is the task whose dry run shows a tier's members' commands: pre-push's own shows only its
// step, which hands pre-push-members to hatch to run, as TestThePushTierIsJudgedByItsCPUTime's
// members that fail show; each other tier runs its members itself.
func dryTier(tier string) string {
	if tier == "pre-push" {
		return "pre-push-members"
	}
	return tier
}

// pushSkips is the tests pre-push leaves to release, as A|B, each over a second, guarding no
// fail-open and in no guard row.
const pushSkips = "TestOnlyThePushTierUsesTheTestCache|TestThePushTiersWallTimeIsOnlyAHangGuard"

// leftOut is the names of the tests the test member leaves out of a tier, read from the go test
// flags of its hatch tests commands as go test reads them (testFlags), or an error where it leaves
// out more or less than the tier should: pre-push (push) by one -skip, exactly ^(pushSkips)$, and
// release by none, and neither by a -run, whose anchors leave tests out as a -skip does.
func leftOut(out []byte, push bool) ([]string, error) {
	// A line ends with a backslash where the command goes on, and go test also reads its flags from
	// GOFLAGS, which the dry run shows only where a command sets it (noGoflags reads the file).
	text := strings.NewReplacer("\\\r\n", " ", "\\\n", " ").Replace(string(out))
	if strings.Contains(text, "GOFLAGS") {
		return nil, fmt.Errorf("a command sets GOFLAGS, which go test reads -skip and -run from")
	}
	var skips, runs []string
	for line := range strings.Lines(text) {
		if !strings.Contains(line, "hatch tests") {
			continue
		}
		s, r, err := testFlags(line)
		if err != nil {
			return nil, err
		}
		skips, runs = append(skips, s...), append(runs, r...)
	}
	if len(runs) > 0 {
		return nil, fmt.Errorf("the test member has -run %q, which leaves tests out", runs)
	}
	if !push {
		if len(skips) > 0 {
			return nil, fmt.Errorf("the test member has -skip %q, and release leaves none out", skips)
		}
		return nil, nil
	}
	if len(skips) != 1 || skips[0] != "^("+pushSkips+")$" {
		return nil, fmt.Errorf("the test member has -skip %q", skips)
	}
	return strings.Split(pushSkips, "|"), nil
}

// noGoflags is an error where Taskfile.yml's text names GOFLAGS, in a task's env: or in a command:
// go test reads -skip and -run from it, as a flag it leaves tests out by, which the dry run leaves out
// of what it prints where GOFLAGS is set in a task's env.
func noGoflags(file string) error {
	if strings.Contains(file, "GOFLAGS") {
		return fmt.Errorf("Taskfile.yml names GOFLAGS, which go test reads -skip and -run from")
	}
	return nil
}

// testFlags is the -skip and the -run values on a command line, as go test reads them: the flag with
// one dash or two, or as -test.skip and -test.run, its value after a space or an =.
func testFlags(line string) (skips, runs []string, err error) {
	w := words(line)
	for i := 0; i < len(w); i++ {
		if !strings.HasPrefix(w[i], "-") {
			continue
		}
		name, val, has := strings.Cut(strings.TrimLeft(w[i], "-"), "=")
		name = strings.TrimPrefix(name, "test.")
		if name != "skip" && name != "run" {
			continue
		}
		if !has {
			if i++; i == len(w) {
				return nil, nil, fmt.Errorf("%s has no value", w[i-1])
			}
			val = w[i]
		}
		if name == "skip" {
			skips = append(skips, val)
		} else {
			runs = append(runs, val)
		}
	}
	return skips, runs, nil
}

// words splits a command line on blanks, a single or double quote holding blanks in a word, as a
// shell does for the commands this file reads.
func words(line string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	in, esc := false, false
	for _, r := range line {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case r == '\\' && quote == 0:
			esc, in = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, in = r, true
		case r == ' ' || r == '\t' || r == '\r' || r == '\n':
			if in {
				out = append(out, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteRune(r)
			in = true
		}
	}
	if in {
		out = append(out, cur.String())
	}
	return out
}

// TestTheTiersTestFlagsAreReadAsGoTestReadsThem holds, for each spelling go test takes, that the
// guard of TestOnlyThePushTierRunsTheTestsWithNoRaceExitSleep sees a test left out: -skip with a
// space or an =, one dash or two, and -test.skip; the -run spellings likewise; and the one list the
// push tier has, in either quote.
//
// What it costs when this fails: a test in a guard row leaves the push tier or the release tier by a
// spelling the guard does not read, and no gate notices.
func TestTheTiersTestFlagsAreReadAsGoTestReadsThem(t *testing.T) {
	t.Parallel()
	const head = "hatch tests --red-when failed -race "
	const list = "'^(" + pushSkips + ")$'"
	for _, c := range []struct {
		name, flags string
		push, ok    bool
	}{
		{"push: the one list", "-skip " + list, true, true},
		{"push: the one list, double-quoted", `-skip "^(` + pushSkips + `)$"`, true, true},
		{"push: the one list by -skip=", "-skip=" + list, true, true},
		{"push: no -skip", "-shuffle=on", true, false},
		{"push: -skip= a second", "-skip " + list + " -skip=TestB", true, false},
		{"push: --skip a second", "-skip " + list + " --skip TestB", true, false},
		{"push: --skip= a second", "-skip " + list + " --skip=TestB", true, false},
		{"push: -test.skip= a second", "-skip " + list + " -test.skip=TestB", true, false},
		{"push: another list", "-skip '^(TestB)$'", true, false},
		{"push: -run", "-skip " + list + " -run '^TestA$'", true, false},
		{"push: -run=", "-skip " + list + " -run=^TestA$", true, false},
		{"push: --run=", "-skip " + list + " --run=^TestA$", true, false},
		{"push: -test.run", "-skip " + list + " -test.run ^TestA$", true, false},
		{"release: no flag", "-shuffle=on -count=1", false, true},
		{"release: -skip", "-skip TestB", false, false},
		{"release: -skip=", "-skip=TestB", false, false},
		{"release: --skip", "--skip TestB", false, false},
		{"release: --skip=", "--skip=TestB", false, false},
		{"release: -test.skip=", "-test.skip=TestB", false, false},
		{"release: -run", "-run '^TestA$'", false, false},
		{"release: -run=", "-run=^TestA$", false, false},
		{"release: --run", "--run ^TestA$", false, false},
		{"release: --test.run=", "--test.run=^TestA$", false, false},
		{"a -skip with no value", "-skip", false, false},
		{"release: -skip on a continued line", "-shuffle=on -count=1 \\\n-skip=TestZ", false, false},
		{"release: -run on a continued line", "-shuffle=on \\\n  -run ^TestA", false, false},
		{"push: -skip on a continued line", "-skip " + list + " \\\n-skip=TestZ", true, false},
		{"push: the list on a continued line", "-skip \\\n" + list, true, true},
		{"release: an export of GOFLAGS", "-shuffle=on\nexport GOFLAGS=-skip=TestZ", false, false},
		{"push: an export of GOFLAGS", "-skip " + list + "\nexport GOFLAGS=-skip=TestZ", true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := leftOut([]byte(head+c.flags+" ./...\n"), c.push)
			if (err == nil) != c.ok {
				t.Errorf("leftOut(%q, push %v) is %v, and want an error: %v", c.flags, c.push, err, !c.ok)
			}
		})
	}
	for name, c := range map[string]struct {
		file string
		ok   bool
	}{
		"an env: of GOFLAGS in the test member": {"  test:\n    env:\n      GOFLAGS: -skip=TestZ\n", false},
		"an export of GOFLAGS in a command":     {"  test:\n    cmds:\n      - export GOFLAGS=-skip=TestZ\n", false},
		"no GOFLAGS":                            {"  test:\n    env:\n      CGO_ENABLED: \"1\"\n", true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := noGoflags(c.file); (err == nil) != c.ok {
				t.Errorf("noGoflags is %v, and want an error: %v", err, !c.ok)
			}
		})
	}
}

// heldTest is whether a test file of this tree declares the top-level test name.
func heldTest(t *testing.T, name string) bool {
	t.Helper()
	decl := []byte("\nfunc " + name + "(t *testing.T) {")
	held := false
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && (d.Name() == ".git" || d.Name() == ".task"):
			return filepath.SkipDir
		case d.IsDir() || !strings.HasSuffix(p, "_test.go"):
			return nil
		}
		b, err := os.ReadFile(p)
		if err == nil && bytes.Contains(b, decl) {
			held = true
		}
		return err
	})
	if err != nil {
		t.Fatalf("reading the tree's test files: %v", err)
	}
	return held
}

// TestThePushAndReleaseTiersHoldTheSize reads pre-push's and release's commands as go-task would run
// them, every member forced so none is left out as up to date (--dry runs nothing), and wants
// measure's size among them: the count is held to its ceiling at each push, not first at the tag.
//
// What it costs when this fails: a pull request takes the module past its ceiling and no gate
// sees it until the release, as main once sat 613 lines past a written ceiling.
func TestThePushAndReleaseTiersHoldTheSize(t *testing.T) {
	t.Parallel()
	for _, tier := range []string{"pre-push", "release"} {
		t.Run(tier, func(t *testing.T) {
			t.Parallel()
			cmd := taskCmd(t, "--dry", "--force", dryTier(tier), "VERSION=v0.0.1")
			isolate(cmd)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("task --dry --force %s: %v\n%s", tier, err, out)
			}
			if !bytes.Contains(out, []byte("go run ./internal/cmd/measure size")) {
				t.Fatalf("task --dry --force %s does not run measure size, so %s passes a tree past the ceiling:\n%s", tier, tier, out)
			}
		})
	}
}

// TestWarmBuildsAndRunsNothing holds warm, the named, untimed step before a timed Tag run,
// to building and no test. go-task's dry run of it runs no task but warm, so no
// member, gate or budget step. Its go commands are these and no other, each a set number of
// times: go list -export, which compiles and runs nothing it builds, once for each system vet's
// and build's loops name and once for each lint's loop names; go vet and go build, once for each
// system of vet's loop and of build's, as those members run them; go test -c, once, the race
// build, which builds and vets the test binaries and runs none; go run of measure with no word,
// which links it and does nothing; and go tool -n, which builds a tool and prints where. No other
// task names warm, so no tier runs it and none of its time is a tier's.
//
// What it costs when this fails: the untimed step runs a test or a gate, so a host's first run of
// the tier, the one that is timed, goes untimed under another name, or a tier pays for the warm-up
// inside its budget. Or warm leaves out a system a member reads, and the timed run builds it.
func TestWarmBuildsAndRunsNothing(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("Taskfile.yml")
	if err != nil {
		t.Fatal(err)
	}
	file := string(raw)
	for _, m := range regexp.MustCompile(`\n  ([a-z-]+):\n`).FindAllStringSubmatch(file, -1) {
		if m[1] == "warm" {
			continue
		}
		for line := range strings.Lines(taskBlock(t, file, m[1])) {
			if l := strings.TrimSpace(line); !strings.HasPrefix(l, "#") && strings.Contains(l, "warm") {
				t.Errorf("task %s names warm: %s", m[1], l)
			}
		}
	}
	// loop is the systems of the one loop over word in the task's script.
	loop := func(task, word string) []string {
		m := regexp.MustCompile(`\n +for ` + word + ` in ([^;\n]+); do\n`).FindStringSubmatch(taskBlock(t, file, task))
		if m == nil {
			t.Fatalf("task %s has no loop over %s", task, word)
		}
		return strings.Fields(m[1])
	}
	vet, build, lint := loop("vet", "target"), loop("build", "target"), loop("lint", "os")
	// Each line warm starts a command with, and how many times, by the systems the members read.
	lines := map[string]int{
		"rm -rf .task/warm": 2,
		"CGO_ENABLED=1 go test -race -c -o .task/warm/ ./...": 1,
	}
	for _, s := range append(append([]string{}, vet...), build...) {
		goos, goarch, _ := strings.Cut(s, "/")
		lines[fmt.Sprintf("GOOS=%s GOARCH=%s go list -export -test -deps ./... > /dev/null", goos, goarch)] = 1
	}
	for _, s := range vet {
		goos, goarch, _ := strings.Cut(s, "/")
		lines[fmt.Sprintf("GOOS=%s GOARCH=%s go vet ./...", goos, goarch)]++
	}
	for _, s := range build {
		goos, goarch, _ := strings.Cut(s, "/")
		lines[fmt.Sprintf("GOOS=%s GOARCH=%s go build ./...", goos, goarch)]++
	}
	for _, goos := range lint {
		lines[fmt.Sprintf("GOOS=%s go list -export -test -deps ./... > /dev/null", goos)]++
	}
	listed := 0
	for l := range lines {
		if strings.Contains(l, "go list") {
			listed++
		}
	}
	// Each go command warm may run, whole, and how many times.
	allowed := map[string]int{
		"go list -export -test -deps ./... > /dev/null": listed,
		"go vet ./...":                                             len(vet),
		"go build ./...":                                           len(build),
		"go test -race -c -o .task/warm/ ./...":                    1,
		"go run ./internal/cmd/measure 2>&1) || true":              1,
		`go tool -modfile="$mod" -n "$tool" > /dev/null || exit 1`: 1,
	}
	cmd := taskCmd(t, "--dry", "warm")
	isolate(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("task --dry warm: %v:\n%s", err, out)
	}
	starts, runs := map[string]int{}, map[string]int{}
	for line := range strings.Lines(string(out)) {
		if strings.HasPrefix(line, "task: [") {
			l, ok := strings.CutPrefix(strings.TrimSpace(line), "task: [warm] ")
			if !ok {
				t.Errorf("warm runs another task: %s", line)
			}
			starts[l]++
		}
		for _, g := range regexp.MustCompile(`\bgo [a-z]+.*`).FindAllString(line, -1) {
			if _, ok := allowed[g]; !ok {
				t.Errorf("warm runs %q, which is none of the builds it may run", g)
			}
			runs[g]++
		}
	}
	for l, want := range lines {
		if starts[l] != want {
			t.Errorf("warm starts a command with %q %d times, want %d:\n%s", l, starts[l], want, out)
		}
	}
	for g, want := range allowed {
		if runs[g] != want {
			t.Errorf("warm runs %q %d times, want %d:\n%s", g, runs[g], want, out)
		}
	}
}

// taskBlock is the task name's lines in file, Taskfile.yml's text: from "  <name>:" to the
// next task's line.
func taskBlock(t *testing.T, file, name string) string {
	t.Helper()
	_, rest, ok := strings.Cut(file, "\n  "+name+":\n")
	if !ok {
		t.Fatalf("Taskfile.yml has no task %s", name)
	}
	if i := regexp.MustCompile(`\n  [a-z-]+:\n`).FindStringIndex(rest); i != nil {
		rest = rest[:i[0]+1]
	}
	return rest
}

// TestOnlyThePushTierUsesTheTestCache reads each tier's commands as go-task would run them (--dry
// runs nothing), with TEST_CACHE=used in the environment as a caller could set it. Pre-push runs the
// tests with go test's cache, so in their written order and with no -count=1; release runs every
// test, in a random order. Each tier, made to run every member, runs govulncheck: pre-push when
// go.mod or go.sum changed, release always.
//
// What it costs when this fails: release reports a package from the cache, or in one order, and a
// tag ships a test that passes only once or only in the order it was written; or a tier drops the
// vulnerability check.
func TestOnlyThePushTierUsesTheTestCache(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		tier   string
		cached bool
	}{
		{"pre-push", true},
		{"release", false},
	} {
		t.Run(c.tier, func(t *testing.T) {
			t.Parallel()
			cmd := taskCmd(t, "--dry", "--force", dryTier(c.tier), "VERSION=v0.0.1")
			isolate(cmd)
			cmd.Env = append(cmd.Env, "TEST_CACHE=used")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("task --dry --force %s: %v\n%s", c.tier, err, out)
			}
			var run string
			for line := range strings.Lines(string(out)) {
				if strings.Contains(line, "hatch tests ") && strings.Contains(line, " -race") {
					run = line
				}
			}
			if run == "" {
				t.Fatalf("task --dry --force %s names no test run, and want its test member's commands:\n%s", c.tier, out)
			}
			if got := !strings.Contains(run, "-count=1") && !strings.Contains(run, "-shuffle"); got != c.cached {
				t.Fatalf("task --dry --force %s: the tests run cached: %v, and want %v:\n%s", c.tier, got, c.cached, run)
			}
			if !bytes.Contains(out, []byte("govulncheck ./...")) {
				t.Fatalf("task --dry --force %s does not run govulncheck:\n%s", c.tier, out)
			}
		})
	}
}

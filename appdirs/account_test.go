package appdirs

import (
	"errors"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test builds the program of testdata/accounthome with the go command and runs it; the module has no runner
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

// AccountHome answers the same with the home's variables set to a folder of the test's and with
// them unset as it did before either: the same absolute path, or ErrNoAccountHome each time, and
// never the folder the environment named. HOME, USERPROFILE, HOMEDRIVE and HOMEPATH are all moved
// on every system, so an AccountHome that read any of them would answer another home, or none.
// The want is AccountHome's own first answer and not os/user's, which a build that falls back to
// HOME would give from the environment, and which Current caches for the life of the process.
//
// What it costs when this fails: a program that asks whose home it runs under, to judge a path the
// environment hands it, is answered by that same environment, and a caller who sets HOME steers
// the folder the program trusts as the account's own.
func TestAccountHomeIsTheAccountsWhateverTheEnvironmentSays(t *testing.T) {
	first, ferr := AccountHome()
	same := func(what, got string, err error) {
		t.Helper()
		if ferr == nil && (err != nil || got != first || !filepath.IsAbs(got)) ||
			ferr != nil && (got != "" || !errors.Is(err, ErrNoAccountHome)) {
			t.Errorf("%s, AccountHome gives %q, %v, and it gave %q, %v before", what, got, err, first, ferr)
		}
	}
	moved := t.TempDir()
	vars := []string{"HOME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH"}
	t.Setenv("HOME", moved) // serial: the process environment, which AccountHome must not read
	t.Setenv("USERPROFILE", moved)
	t.Setenv("HOMEDRIVE", moved)
	t.Setenv("HOMEPATH", moved)
	set, err := AccountHome()
	same("with "+strings.Join(vars, ", ")+" set to "+moved, set, err)
	if set == moved {
		t.Errorf("with %v set to %s, AccountHome gives that folder", vars, moved)
	}
	for _, v := range vars {
		if err := os.Unsetenv(v); err != nil {
			t.Fatal(err)
		}
	}
	unset, err := AccountHome()
	same("with "+strings.Join(vars, ", ")+" unset", unset, err)
}

// A program built with the osusergo tag and without cgo, the build where os/user's Current falls
// back to HOME where /etc/passwd does not hold the account, and LookupId asks Current first, run
// with HOME, USERPROFILE, HOMEDRIVE, HOMEPATH set to a folder of the test's and USER set, never
// answers that folder. Its answer is what accountWithoutCgo says, which is not read from os/user:
// ErrNoAccountHome on macOS, whose accounts are not in /etc/passwd, /etc/passwd's home for the uid,
// as the test reads it, on the other Unix systems, and the token's profile folder on Windows.
//
// What it costs when this fails: a program built without cgo, the default of a cross build and of
// a static one, answers the environment's HOME as the account's home wherever the account is in
// LDAP or sssd, or the system is macOS, and a caller who sets HOME steers it.
func TestAProgramBuiltWithOsusergoAndWithoutCgoNeverAnswersHOME(t *testing.T) {
	t.Parallel()
	prog := accountProgram(t)
	planted := t.TempDir()
	cmd := exec.CommandContext(t.Context(), prog) //nolint:gosec // G204: the program this test built

	cmd.Env = append(os.Environ(), "HOME="+planted, "USERPROFILE="+planted, "HOMEDRIVE="+planted, "HOMEPATH="+planted, //nolint:forbidigo // cli.config.3: the program's environment, the test's with the home's variables moved
		"USER=appdirs-test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v: %s", prog, err, out)
	}
	home, _, _ := strings.Cut(strings.TrimPrefix(string(out), "home="), "\n")
	refused := strings.Contains(string(out), "\nrefused=true\n")
	if strings.HasPrefix(home, planted) {
		t.Errorf("the program answers %q, beneath the planted HOME %s:\n%s", home, planted, out)
	}
	want, werr := accountWithoutCgo(t)
	if home != want || refused != (werr != nil) {
		t.Errorf("the program printed\n%s\nand want home %q, ErrNoAccountHome %t (%v)", out, want, werr != nil, werr)
	}
}

// accountProgram builds testdata/accounthome with the osusergo tag and CGO_ENABLED=0, as a
// program is built, not as a test binary, and answers its path.
func accountProgram(t *testing.T) string {
	t.Helper()
	// go test's cache keys a passing run on what the test process opens, not on what go build reads,
	// so the test reads the program's folder, and the package's, itself: an open
	// folder is keyed on each of its files' size and time.
	for _, dir := range []string{".", filepath.Join("testdata", "accounthome")} {
		if _, err := os.ReadDir(dir); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	prog := filepath.Join(dir, "accounthome.exe") // the name Windows runs, which the others run as well

	cmd := exec.CommandContext(t.Context(), "go", "build", "-tags", "osusergo", "-o", prog, "./testdata/accounthome") //nolint:gosec // G204: the go command, building this package's own testdata
	// The environment is testkit's, which pins the go command's module and build caches where the
	// run had them; GOPROXY=off keeps the build off the network, and GOPATH is the test's own, as
	// trust's overflow program is built.
	cmd.Env = append(os.Environ(), "GOWORK=off", "CGO_ENABLED=0", "GOPROXY=off", //nolint:forbidigo // cli.config.3: the go command reads the environment the tests were started in
		"GOPATH="+filepath.Join(dir, "gopath"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./testdata/accounthome: %v: %s", err, out)
	}
	return prog
}

// A lookup that fails, and a home that is empty, not an absolute path, or not clean, are
// ErrNoAccountHome, the lookup's own error kept as the cause; an absolute, clean home is answered
// as the system gave it.
//
// What it costs when this fails: a program handed "" or a relative home by an account record
// keeps its files under the working folder, one handed a home with .. in it is answered another
// folder than the record's where a link is on the way, or it tells a lookup that failed from one
// that answered only by the error's text.
func TestAccountHomeRefusesALookupThatFailsAndAnEmptyRelativeOrUncleanHome(t *testing.T) {
	t.Parallel()
	_, err := homeOf("", user.UnknownUserIdError(7))
	var unknown user.UnknownUserIdError
	if !errors.Is(err, ErrNoAccountHome) || !errors.As(err, &unknown) {
		t.Errorf("a failed lookup gives %v, want ErrNoAccountHome with the lookup's error", err)
	}
	abs := t.TempDir()
	sep := string(filepath.Separator)
	for _, home := range []string{"", ".", "home/u", filepath.Join("..", "u"), abs + sep, abs + sep + ".." + sep + "v", abs + sep + "." + sep + "v"} {
		got, err := homeOf(home, nil)
		if got != "" || !errors.Is(err, ErrNoAccountHome) {
			t.Errorf("home %q gives %q, %v, want ErrNoAccountHome", home, got, err)
		}
	}
	if got, err := homeOf(abs, nil); got != abs || err != nil {
		t.Errorf("home %q gives %q, %v, want it as given", abs, got, err)
	}
}

package confine

import (
	"errors"
	"io/fs"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test starts unshare, with this test binary again in it, as trust's namespace rows do; the module has no runner
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// finalFoldersOnly is whether Final answers only a folder here, and refuses a file: Linux answers a file, one with a second name by the name it was opened by.
const finalFoldersOnly = false

func TestOnLinuxFinalAnswersANameThisAccountMayNotRead(t *testing.T) {
	t.Parallel()
	tmp := finalTemp(t)
	file, folder := filepath.Join(tmp, "file"), filepath.Join(tmp, "folder")
	if err := os.WriteFile(file, nil, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(folder, 0o300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(folder, 0o700) })
	for _, p := range []string{file, folder} {
		if got := finalOK(t, p); got != p {
			t.Errorf("Final of %s, which this account may not read, = %q, and want its own name", p, got)
		}
	}
}

func TestOnLinuxFinalWithNoProcIsErrUnsupported(t *testing.T) {
	t.Parallel()
	tmp := finalTemp(t)
	noProc := filepath.Join(tmp, "proc")
	_, err := final(tmp, func(f *os.File) (string, error) { return procName(noProc, f) }, os.Lstat)
	if !errors.Is(err, errors.ErrUnsupported) || errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Final where /proc is not there: %v, and want errors.ErrUnsupported and no match of fs.ErrNotExist", err)
	}
}

// fdLinks writes in fds a link for each descriptor a test may open, 0 to 255, each naming file
// through via, a link to file's folder, so os.SameFile alone would pass the name it reads.
func fdLinks(t *testing.T, fds, via string) {
	t.Helper()
	if err := os.MkdirAll(fds, 0o700); err != nil {
		t.Fatal(err)
	}
	for n := range 256 {
		if err := os.Symlink(filepath.Join(via, "file"), filepath.Join(fds, strconv.Itoa(n))); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOnLinuxFinalRefusesAProcThatIsNotAProcfs(t *testing.T) {
	t.Parallel()
	tmp := finalTemp(t)
	if err := os.WriteFile(filepath.Join(tmp, "file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(tmp, filepath.Join(tmp, "via")); err != nil {
		t.Fatal(err)
	}
	// proc stands where a tmpfs mounted over /proc would, its links self/fd/<n> naming the file
	// through a linked parent.
	proc := filepath.Join(tmp, "proc")
	fdLinks(t, filepath.Join(proc, "self", "fd"), filepath.Join(tmp, "via"))
	got, err := final(filepath.Join(tmp, "file"), func(f *os.File) (string, error) { return procName(proc, f) }, os.Lstat)
	if !errors.Is(err, errors.ErrUnsupported) || got != "" {
		t.Errorf("Final where /proc is a folder of another file system = %q, %v, and want errors.ErrUnsupported", got, err)
	}
}

const (
	finalProcTest = "TestOnLinuxFinalCrossesNoMountOnTheWayToProcSelfFd"
	finalProcArg  = "confine-final-proc-namespace"
)

// What it costs when this fails: an owner of this mount namespace binds a folder of its own links
// over /proc/self/fd, or mounts a tmpfs over /proc with links at self/fd, and Final answers the name
// it wrote, of a second name of the file, as the kernel's: a journal keys one file by two names.
func TestOnLinuxFinalCrossesNoMountOnTheWayToProcSelfFd(t *testing.T) {
	if i := slices.Index(os.Args, finalProcArg); i > 0 && i+1 < len(os.Args) {
		finalOverProc(t, os.Args[i+1])
		return
	}
	t.Parallel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"--user", "--map-root-user", "--mount", exe, "-test.run=^" + finalProcTest + "$", "-test.count=1", "-test.v", finalProcArg, finalTemp(t)}
	out, err := exec.CommandContext(t.Context(), "unshare", args...).CombinedOutput() //nolint:gosec // G204: unshare and this test binary, with literal flags and the test's own folder
	if err != nil || !strings.Contains(string(out), "the mounted-over rows ran") {
		t.Errorf("this test in unshare --user --map-root-user --mount: %v:\n%s", err, out)
	}
	for line := range strings.Lines(string(out)) {
		if _, row, ok := strings.Cut(line, "the link row: "); ok {
			t.Logf("the link row: %s", strings.TrimSpace(row))
		}
	}
}

// finalOverProc is the test's rows in its own user and mount namespace, each mount over the last:
// the name answered before any; a folder of links bound over /proc/self/fd, each naming the file
// through a linked parent, which only openat2's RESOLVE_NO_XDEV refuses; and a tmpfs over /proc
// with the same links at self/fd, which procfs.Root's magic refuses.
func finalOverProc(t *testing.T, d string) {
	t.Helper()
	file, via := filepath.Join(d, "file"), filepath.Join(d, "via")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(d, via); err != nil {
		t.Fatal(err)
	}
	refused := func(row string) {
		t.Helper()
		if got, err := Final(file); !errors.Is(err, errors.ErrUnsupported) || got != "" {
			t.Errorf("%s: Final(%s) = %q, %v, and want errors.ErrUnsupported", row, file, got, err)
		}
	}
	mount := func(src, dst, typ string, flags uintptr) {
		t.Helper()
		if err := unix.Mount(src, dst, typ, flags, ""); err != nil {
			t.Fatalf("mount %s on %s: %v", src, dst, err)
		}
	}
	if got, err := Final(file); err != nil || got != file {
		t.Errorf("before any mount: Final(%s) = %q, %v, and want its own name", file, got, err)
	}

	if linkOverFd(t, d, via) {
		refused("a link mounted on /proc/self/fd/<n> itself, for the descriptor Final opens next")
	}

	fds := filepath.Join(d, "fds")
	fdLinks(t, fds, via)
	mount(fds, "/proc/self/fd", "", unix.MS_BIND)
	if target, err := os.Readlink("/proc/self/fd/0"); err != nil || target != filepath.Join(via, "file") {
		t.Fatalf("/proc/self/fd/0 by its path: %q, %v, and want %s, so the bind took", target, err, filepath.Join(via, "file"))
	}
	refused("a folder of links bound over /proc/self/fd")

	mount("tmpfs", "/proc", "tmpfs", 0)
	fdLinks(t, "/proc/self/fd", via)
	refused("a tmpfs over /proc, with links at self/fd")
	t.Logf("the mounted-over rows ran")
}

// linkOverFd mounts a link naming the file through via on the link /proc/self/fd/<n> itself, n
// being the descriptor Final opens next: a file holds n while the mount is made, by open_tree's
// clone of the link, not followed, and move_mount onto the link, not followed, and is closed after.
// Only openat2's RESOLVE_NO_XDEV on the last name, <n>, refuses it. It answers whether the mount
// took: from Linux 6.12 the kernel refuses a mount on /proc/<pid>/fd/<n> as ENOENT, and then the
// same mount onto a link of d's is made, so the refusal is the kernel's and not the call's.
func linkOverFd(t *testing.T, d, via string) bool {
	t.Helper()
	link, want := filepath.Join(d, "link"), filepath.Join(via, "file")
	if err := os.Symlink(want, link); err != nil {
		t.Fatal(err)
	}
	hold, err := os.Open(filepath.Join(d, "file"))
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	on := "/proc/self/fd/" + strconv.FormatUint(uint64(hold.Fd()), 10)
	tree, err := unix.OpenTree(unix.AT_FDCWD, link, unix.OPEN_TREE_CLONE|unix.OPEN_TREE_CLOEXEC|unix.AT_SYMLINK_NOFOLLOW)
	if err != nil {
		t.Fatalf("open_tree of %s: %v", link, err)
	}
	defer unix.Close(tree)
	if err := unix.MoveMount(tree, "", unix.AT_FDCWD, on, unix.MOVE_MOUNT_F_EMPTY_PATH); errors.Is(err, unix.ENOENT) {
		plain := filepath.Join(d, "plain")
		if err := os.Symlink("/nowhere", plain); err != nil {
			t.Fatal(err)
		}
		if err := unix.MoveMount(tree, "", unix.AT_FDCWD, plain, unix.MOVE_MOUNT_F_EMPTY_PATH); err != nil {
			t.Fatalf("move_mount of %s onto %s, after %s was ENOENT: %v", link, plain, on, err)
		}
		if target, err := os.Readlink(plain); err != nil || target != want {
			t.Fatalf("%s by its path: %q, %v, and want %s, so the mount took", plain, target, err, want)
		}
		var u unix.Utsname
		_ = unix.Uname(&u)
		t.Logf("the link row: the kernel, %s, refuses a mount on %s itself, as ENOENT, and takes it on a link of the test's", unix.ByteSliceToString(u.Release[:]), on)
		return false
	} else if err != nil {
		t.Fatalf("move_mount of %s onto %s: %v", link, on, err)
	}
	if target, err := os.Readlink(on); err != nil || target != want {
		t.Fatalf("%s by its path: %q, %v, and want %s, so the mount took", on, target, err, want)
	}
	if err := hold.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("the link row: ran, with a link mounted on %s", on)
	return true
}

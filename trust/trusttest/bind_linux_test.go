package trusttest_test

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec" //nolint:depguard // cli.process.1: the test starts unshare, with this test binary again in it, as the mount rows do; the module has no runner
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/birdtable/go-files/trust"
	"github.com/birdtable/go-files/trust/trusttest"
)

// bindTest is the test run again in the namespaces unshare makes; bindArg marks the run that keeps
// the capabilities the namespace grants, noCapsArg the run that drops them at its exec, lockedArg
// the run below a namespace that locks nodev, and rootArg the run in one that maps root.
const (
	bindTest       = "TestBindMountsEachBoundOverAnEntryInTheTestsOwnNamespace"
	bindArg        = "trusttest-bind"
	noCapsArg      = "trusttest-bind-no-caps"
	lockedArg      = "trusttest-bind-locked"
	rootArg        = "trusttest-bind-root-mapped"
	holdArg        = "trusttest-bind-hold"
	bindReady      = "BIND-READY"
	bindHeld       = "BIND-HELD"
	bindSkipped    = "BIND-SKIPPED"
	bindFailed     = "BIND-FAILED"
	notHere        = "not in a mount namespace this process may mount in"
	firstNamespace = "this is the first user namespace, whose mounts are the system's"
)

// Bind mounts, with no root, in the namespace unshare --user --map-current-user --mount
// --keep-caps makes, a folder of the test's temp folder over /mnt or /srv, whatever it holds, with
// each Bound's flag shown on the mount, kept until the test's end: there the entry passes as a
// folder of Who.Mounts, a name in dir is refused with ErrMount without it and passes with it, all
// with Who.Overflow, and the mount is gone at the end. An entry one Bind holds is no other's until
// its test ends, whichever process's in the namespace holds it: an entry another process's Bind
// holds skips, and of two Binds whose mounts meet at an entry, as two processes' at once, the
// earliest keeps it and the later undoes its own and goes on, its end failing on no mount that went
// off with the one beneath it. A flag the kernel locks on the temp folder's mount, nodev set in a namespace
// above, is kept on the mount Bind makes. It skips, saying why, in the first user namespace, which
// this process's own is where /proc/self/uid_map maps every id to itself; in one that maps root,
// as unshare --map-root-user makes; in one that drops the capability to mount, as unshare does
// without --keep-caps and go-proc's testenv.Contain does before its tests; where no folder
// qualifies as the entry; where the temp folder is on a tmpfs, whose flags trust does not judge;
// and, for NoexecWithoutNosuid, where it is on a disk mounted nosuid. The runs in a namespace make
// their temp folders on a disk, though TMPDIR is a tmpfs.
//
// What it costs when this fails: a test that asks for a nosuid mount, or one noexec without
// nosuid, judges a disk with neither, or one it cannot name in Who.Mounts, and says a refusal
// trust makes holds where it does not; or Bind mounts over the system's /mnt, or fails a runner
// that cannot mount, where it is to skip; or two test binaries in one namespace
// stack a Bind's mount on another's, and one's unmount takes off the other's.
func TestBindMountsEachBoundOverAnEntryInTheTestsOwnNamespace(t *testing.T) {
	switch {
	case slices.Contains(os.Args, bindArg):
		boundRows(t)
		crossRows(t)
		stackRows(t)
		t.Logf("the Bind rows ran")
		return
	case slices.Contains(os.Args, holdArg):
		holdRow(t)
		return
	case slices.Contains(os.Args, noCapsArg):
		r := call(t, func(r *recorder) { trusttest.Bind(r, trusttest.Nosuid) })
		r.end()
		if !r.skipped || r.failed || !r.says(notHere) || !r.says("operation not permitted") {
			t.Errorf("Bind with no capability to mount skipped %v, failed %v, %q, and want it skipped saying the mount was refused", r.skipped, r.failed, r.said)
		}
		t.Logf("the no-capability row ran")
		return
	case slices.Contains(os.Args, rootArg):
		r := call(t, func(r *recorder) { trusttest.Bind(r, trusttest.Nosuid) })
		r.end()
		if !r.skipped || r.failed || !r.says(notHere) || !r.says("mapping this account and no root") {
			t.Errorf("Bind in a namespace that maps root skipped %v, failed %v, %q, and want it skipped saying so", r.skipped, r.failed, r.said)
		}
		t.Logf("the root-mapped row ran")
		return
	case slices.Contains(os.Args, lockedArg):
		var entry, dir string
		r := call(t, func(r *recorder) { entry, dir = trusttest.Bind(r, trusttest.Nosuid) })
		if r.failed || r.skipped {
			r.end()
			t.Fatalf("Bind on a temp folder whose nodev the kernel locks failed %v, skipped %v: %q", r.failed, r.skipped, r.said)
		}
		judgeBound(t, entry, dir, unix.ST_NOSUID|unix.ST_NODEV, 0)
		r.end()
		t.Logf("the locked-flag row ran")
		return
	}
	t.Parallel()
	m, err := os.ReadFile("/proc/self/uid_map")
	if err != nil {
		t.Fatal(err)
	}
	first := slices.Equal(strings.Fields(string(m)), []string{"0", "0", "4294967295"})
	r := call(t, func(r *recorder) { trusttest.Bind(r, trusttest.Nosuid) })
	r.end()
	if !r.skipped || r.failed || !r.says(notHere) || first && !r.says(firstNamespace) {
		t.Errorf("Bind in this process's own namespace, whose uid_map is %q, skipped %v, failed %v, %q, and want it skipped saying why", m, r.skipped, r.failed, r.said)
	}
	tmp := diskTemp(t)
	// No skip: a runner that cannot unshare fails these rows, as it fails trust's namespace rows.
	for _, run := range []struct{ arg, ran string }{{bindArg, "the Bind rows ran"}, {noCapsArg, "the no-capability row ran"}, {rootArg, "the root-mapped row ran"}} {
		args := []string{"--user", "--map-current-user", "--mount"}
		if run.arg == rootArg {
			args[1] = "--map-root-user"
		}
		if run.arg != noCapsArg {
			args = append(args, "--keep-caps")
		}
		if out, err := rerun(t, tmp, args, run.arg); err != nil || !strings.Contains(string(out), run.ran) {
			t.Errorf("this test in unshare %s, its temp folders on %s: %v:\n%s", strings.Join(args, " "), tmp, err, out)
		}
	}
}

// rerun runs this test binary again, in unshare with args, with arg to say which run it is and
// TMPDIR set to tmp, and gives what it printed.
func rerun(t *testing.T, tmp string, args []string, arg string) ([]byte, error) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "unshare", append(slices.Clone(args), exe, "-test.run=^"+bindTest+"$", "-test.count=1", "-test.v", arg)...) //nolint:gosec // G204: unshare and this test binary, with literal flags
	cmd.Env = append(cmd.Environ(), "TMPDIR="+tmp)
	return cmd.CombinedOutput()
}

// diskTemp is a folder of the test's on a disk, for the runs in a namespace to make their temp
// folders in: the test's temp folder, or, where TMPDIR is a tmpfs or an overlay, as /tmp is on
// Fedora and Debian 13 and / in a container, one in the package's folder. No disk fails the test.
func diskTemp(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if onDisk(d) {
		return d
	}
	wd, err := os.Getwd()
	if err == nil {
		d, err = os.MkdirTemp(wd, ".bind-")
	}
	if err != nil {
		t.Fatalf("TMPDIR is no disk, and no folder is made beside the test: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(d); err != nil {
			t.Errorf("remove %s: %v", d, err)
		}
	})
	if !onDisk(d) {
		t.Fatalf("neither TMPDIR nor %s is on a disk, and the Bind rows want one", wd)
	}
	return d
}

// onDisk says whether d is on a file system that is neither a tmpfs nor an overlay.
func onDisk(d string) bool {
	var sf unix.Statfs_t
	return unix.Statfs(d, &sf) == nil && sf.Type != unix.TMPFS_MAGIC && sf.Type != unix.OVERLAYFS_SUPER_MAGIC
}

// mountID is the id of the mount p is on.
func mountID(t *testing.T, p string) uint64 {
	t.Helper()
	var sx unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, p, 0, unix.STATX_MNT_ID, &sx); err != nil || sx.Mask&unix.STATX_MNT_ID == 0 {
		t.Fatalf("statx %s: %v, and want its mount id", p, err)
	}
	return sx.Mnt_id
}

// boundRows is the rows in the namespace that keeps its capabilities: each Bound bound, judged and
// unmounted, then each skip a namespace may reach.
func boundRows(t *testing.T) {
	t.Helper()
	for _, c := range []struct {
		name     string
		how      trusttest.Bound
		set, not int64
	}{
		{"nosuid", trusttest.Nosuid, unix.ST_NOSUID, 0},
		{"noexec and nodev without nosuid", trusttest.NoexecWithoutNosuid, unix.ST_NOEXEC | unix.ST_NODEV, unix.ST_NOSUID},
	} {
		t.Run(c.name, func(t *testing.T) {
			var entry, dir string
			r := call(t, func(r *recorder) { entry, dir = trusttest.Bind(r, c.how) })
			if r.failed || r.skipped {
				r.end()
				t.Fatalf("Bind failed %v, skipped %v: %q", r.failed, r.skipped, r.said)
			}
			judgeBound(t, entry, dir, c.set, c.not)
			bound := mountID(t, entry)
			r.end()
			if after := mountID(t, entry); after == bound {
				t.Errorf("%s after the test's end is on mount %d still, and want it unmounted", entry, after)
			}
		})
	}
	t.Run("an entry another Bind holds", func(t *testing.T) {
		var entry string
		r := call(t, func(r *recorder) { entry, _ = trusttest.Bind(r, trusttest.Nosuid) })
		if r.failed || r.skipped {
			r.end()
			t.Fatalf("Bind failed %v, skipped %v: %q", r.failed, r.skipped, r.said)
		}
		second := call(t, func(r *recorder) { trusttest.BindOver(r, trusttest.Nosuid, []string{entry}) })
		second.end()
		r.end()
		if !second.skipped || second.failed || !second.says(entry+" is another test's Bind's") {
			t.Errorf("a second Bind over %s, which the first holds, skipped %v, failed %v, %q, and want it skipped saying so", entry, second.skipped, second.failed, second.said)
		}
	})
	t.Run("a temp folder mounted nodev, which a namespace below locks", func(t *testing.T) {
		src := t.TempDir()
		var sf unix.Statfs_t
		if err := unix.Statfs(src, &sf); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mount(src, src, "", unix.MS_BIND, ""); err != nil {
			t.Fatalf("bind %s over itself: %v", src, err)
		}
		t.Cleanup(func() {
			if err := unix.Unmount(src, unix.MNT_DETACH); err != nil {
				t.Errorf("unmount %s: %v", src, err)
			}
		})
		if err := unix.Mount("", src, "", unix.MS_REMOUNT|unix.MS_BIND|unix.MS_NODEV|keptFlags(int64(sf.Flags)), ""); err != nil { // int32 on 32-bit Linux
			t.Fatalf("remount %s nodev: %v", src, err)
		}
		// The run below makes its temp folders on src; this one's are made already.
		out, err := rerun(t, src, []string{"--user", "--map-current-user", "--mount", "--keep-caps"}, lockedArg)
		if err != nil || !strings.Contains(string(out), "the locked-flag row ran") {
			t.Errorf("this test in a namespace below, its temp folder on %s: %v:\n%s", src, err, out)
		}
	})
	t.Run("no folder qualifies", func(t *testing.T) {
		mine := t.TempDir()
		gone := filepath.Join(mine, "gone")
		_, why := trust.Path(mine, trust.Folder, trust.Who{RootOnly: true, Overflow: true})
		_, whyGone := trust.Path(gone, trust.Folder, trust.Who{RootOnly: true, Overflow: true})
		r := call(t, func(r *recorder) { trusttest.BindOver(r, trusttest.Nosuid, []string{mine, gone}) })
		r.end()
		if why == nil || whyGone == nil || !r.skipped || r.failed || !r.says("no folder qualifies") || !r.says(why.Error()+"; "+whyGone.Error()) {
			t.Errorf("Bind over %s and %s skipped %v, failed %v, %q, and want it skipped naming why each fails", mine, gone, r.skipped, r.failed, r.said)
		}
	})
	for _, c := range []struct {
		name string
		how  trusttest.Bound
		says string
	}{
		{"a tmpfs temp folder", trusttest.Nosuid, "whose flags trust does not judge"},
		{"a temp folder mounted nosuid", trusttest.NoexecWithoutNosuid, "mounted nosuid, which no namespace lifts"},
	} {
		t.Run(c.name, func(t *testing.T) {
			tmp := t.TempDir()
			ms := uintptr(0)
			if c.how == trusttest.NoexecWithoutNosuid {
				ms = unix.MS_NOSUID
			}
			if err := unix.Mount("tmpfs", tmp, "tmpfs", ms, ""); err != nil {
				t.Fatalf("mount a tmpfs on %s: %v", tmp, err)
			}
			t.Cleanup(func() {
				if err := unix.Unmount(tmp, unix.MNT_DETACH); err != nil {
					t.Errorf("unmount %s: %v", tmp, err)
				}
			})
			t.Run("bound", func(t *testing.T) { // a test of its own, whose temp folder is made in TMPDIR
				t.Setenv("TMPDIR", tmp)
				r := call(t, func(r *recorder) { trusttest.Bind(r, c.how) })
				r.end()
				if !r.skipped || r.failed || !r.says(c.says) {
					t.Errorf("Bind on %s skipped %v, failed %v, %q, and want it skipped saying %q", c.name, r.skipped, r.failed, r.said, c.says)
				}
			})
		})
	}
}

// judgeBound fails t unless entry, which Bind gave, is /mnt or /srv, mounted with the statfs
// flags set and without not, passing as a folder of Who.Mounts, and dir is a folder of this
// account's in it, where a key is refused with ErrMount without Who.Mounts and passes with it.
func judgeBound(t *testing.T, entry, dir string, set, not int64) {
	t.Helper()
	if entry != "/mnt" && entry != "/srv" || filepath.Dir(dir) != entry {
		t.Errorf("Bind gave %s and %s, and want /mnt or /srv, and a folder in it", entry, dir)
	}
	var sf unix.Statfs_t
	if err := unix.Statfs(entry, &sf); err != nil {
		t.Fatal(err)
	}
	if f := int64(sf.Flags); f&set != set || f&not != 0 { // int32 on 32-bit Linux
		t.Errorf("%s is mounted with the statfs flags %#x, and want %#x set and %#x not", entry, f, set, not)
	}
	var st unix.Stat_t
	if err := unix.Stat(dir, &st); err != nil || int(st.Uid) != os.Geteuid() {
		t.Errorf("%s is of uid %d, %v, and want this account's, %d", dir, st.Uid, err, os.Geteuid())
	}
	k := filepath.Join(dir, "key")
	if err := os.WriteFile(k, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	with := trust.Who{Overflow: true, Mounts: []string{entry}}
	if _, err := trust.Path(dir, trust.Folder, with); err != nil {
		t.Errorf("%s, with Who.Mounts naming %s: %v, and want a pass, the entry judged as any is", dir, entry, err)
	}
	if _, err := trust.Path(k, trust.Key, trust.Who{Overflow: true}); !errors.Is(err, trust.ErrMount) {
		t.Errorf("%s, with no Who.Mounts: %v, and want ErrMount", k, err)
	}
	if _, err := trust.Path(k, trust.Key, with); err != nil {
		t.Errorf("%s, with Who.Mounts naming %s: %v, and want a pass", k, entry, err)
	}
	me := "uid:" + strconv.Itoa(os.Geteuid())
	if b, err := trust.ReadOwned(k, me, true, 3, trust.Who{}, with); err != nil || string(b) != "key" {
		t.Errorf("ReadOwned of %s, with Who.Mounts naming %s and Who.Overflow: %q, %v, and want it read", k, entry, b, err)
	}
	if b, err := trust.ReadOwned(k, me, true, 3, trust.Who{Overflow: true}); b != nil || !errors.Is(err, trust.ErrMount) {
		t.Errorf("ReadOwned of %s, with no Who.Mounts: %q, %v, and want ErrMount, nothing read", k, b, err)
	}
}

// holdRow is the run whose Bind is another process's: it binds the entry its arguments name, with
// Bind's checks before the mount dropped and after a line on stdin where they say blind, says what
// came of it, and holds what it bound until stdin ends.
func holdRow(t *testing.T) {
	t.Helper()
	var e string
	for _, a := range os.Args {
		if v, ok := strings.CutPrefix(a, "entry="); ok {
			e = v
		}
	}
	blind := slices.Contains(os.Args, "blind")
	in := bufio.NewReader(os.Stdin)
	if blind {
		t.Logf("%s", bindReady)
		if _, err := in.ReadString('\n'); err != nil {
			t.Fatalf("the go line: %v", err)
		}
	}
	r := call(t, func(r *recorder) { trusttest.BindHooked(r, trusttest.Nosuid, []string{e}, blind, nil) })
	switch {
	case r.failed: // said too, so the test that waits on this run never waits on it for ever
		t.Errorf("%s Bind over %s: %q", bindFailed, e, r.said)
	case r.skipped:
		t.Logf("%s %q", bindSkipped, r.said)
	default:
		t.Logf("%s %s", bindHeld, e)
	}
	_, err := io.Copy(io.Discard, in) // until the test that started this run lets it go
	r.end()
	if err != nil || r.failed {
		t.Errorf("holding %s: %v, and its end failed %v: %q", e, err, r.failed, r.said)
	}
}

// holder is a run of this test binary, in this test's namespace, whose Bind binds an entry, with
// what it printed.
type holder struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Scanner
	said []string
}

// startHolder starts a run whose Bind binds e, blind where blind is set.
func startHolder(t *testing.T, e string, blind bool) *holder {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-test.run=^" + bindTest + "$", "-test.count=1", "-test.v", holdArg, "entry=" + e}
	if blind {
		args = append(args, "blind")
	}
	h := &holder{cmd: exec.CommandContext(t.Context(), exe, args...)} //nolint:gosec // G204: this test binary, with literal flags
	if h.in, err = h.cmd.StdinPipe(); err != nil {
		t.Fatal(err)
	}
	out, err := h.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	h.cmd.Stderr = h.cmd.Stdout
	h.out = bufio.NewScanner(out)
	if err := h.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.in.Close(); _ = h.cmd.Wait() }) // after end, a no-op; before it, a test that failed
	return h
}

// wait is the first line the run prints that holds one of words, or fails t where it ends first.
func (h *holder) wait(t *testing.T, words ...string) string {
	t.Helper()
	for h.out.Scan() {
		l := h.out.Text()
		h.said = append(h.said, l)
		for _, w := range words {
			if strings.Contains(l, w) {
				return l
			}
		}
	}
	t.Fatalf("a holding run ended before it said any of %q:\n%s", words, strings.Join(h.said, "\n"))
	return ""
}

// end lets the run go, at which its test ends, and fails t unless it passed.
func (h *holder) end(t *testing.T) {
	t.Helper()
	if err := h.in.Close(); err != nil {
		t.Error(err)
	}
	for h.out.Scan() {
		h.said = append(h.said, h.out.Text())
	}
	if err := h.cmd.Wait(); err != nil {
		t.Errorf("a holding run: %v:\n%s", err, strings.Join(h.said, "\n"))
	}
}

// freeEntries is the first n of /mnt, /srv, /opt and /media that are folders real root made, as
// Bind's entries are, or fails t.
func freeEntries(t *testing.T, n int) []string {
	t.Helper()
	var es []string
	for _, e := range []string{"/mnt", "/srv", "/opt", "/media"} {
		if _, err := trust.Path(e, trust.Folder, trust.Who{RootOnly: true, Overflow: true}); err == nil {
			es = append(es, e)
		}
		if len(es) == n {
			return es
		}
	}
	t.Fatalf("of /mnt, /srv, /opt and /media, %q qualify as Bind's entries, and the row wants %d", es, n)
	return nil
}

// crossRows is the rows of Binds of two processes that meet: another process's holding an entry,
// and two processes' over one entry at once.
func crossRows(t *testing.T) {
	t.Helper()
	t.Run("an entry another process's Bind holds", func(t *testing.T) {
		e := freeEntries(t, 1)[0]
		h := startHolder(t, e, false)
		if l := h.wait(t, bindHeld, bindSkipped, bindFailed); !strings.Contains(l, bindHeld) {
			t.Fatalf("the other process's Bind over %s: %s", e, l)
		}
		r := call(t, func(r *recorder) { trusttest.BindOver(r, trusttest.Nosuid, []string{e}) })
		r.end()
		h.end(t)
		if !r.skipped || r.failed || !r.says(e+" is held by another process's Bind in this namespace") {
			t.Errorf("a Bind over %s, which another process's holds, skipped %v, failed %v, %q, and want it skipped saying so", e, r.skipped, r.failed, r.said)
		}
	})
	t.Run("two processes' Binds over one entry at once", func(t *testing.T) {
		e := freeEntries(t, 1)[0]
		hs := []*holder{startHolder(t, e, true), startHolder(t, e, true)}
		for _, h := range hs {
			h.wait(t, bindReady)
		}
		for _, h := range hs {
			if _, err := io.WriteString(h.in, "go\n"); err != nil {
				t.Fatal(err)
			}
		}
		var held, lost int
		for _, h := range hs {
			switch l := h.wait(t, bindHeld, bindSkipped, bindFailed); {
			case strings.Contains(l, bindHeld):
				held++
			case strings.Contains(l, e+" was bound by another Bind first"):
				lost++
			default:
				t.Errorf("a Bind over %s: %s", e, l)
			}
		}
		for _, h := range hs {
			h.end(t)
		}
		if held != 1 || lost != 1 {
			t.Errorf("two processes' Binds over %s at once: %d held it and %d skipped saying the other was first, and want one each", e, held, lost)
		}
	})
}

// stackRows is the rows of one process's Binds, one blind to the other's checks, whose mounts
// stack at an entry: one over the other's, and one whose mount went off with the one beneath it.
func stackRows(t *testing.T) {
	t.Helper()
	t.Run("a Bind's mount over another's in one process", func(t *testing.T) {
		e := freeEntries(t, 1)[0]
		var dir string
		first := call(t, func(r *recorder) { _, dir = trusttest.BindOver(r, trusttest.Nosuid, []string{e}) })
		if first.failed || first.skipped {
			first.end()
			t.Fatalf("Bind failed %v, skipped %v: %q", first.failed, first.skipped, first.said)
		}
		was := mountID(t, e)
		second := call(t, func(r *recorder) { trusttest.BindHooked(r, trusttest.Nosuid, []string{e}, true, nil) })
		second.end()
		now := mountID(t, e)
		_, err := trust.Path(dir, trust.Folder, trust.Who{Overflow: true, Mounts: []string{e}})
		first.end()
		if after := mountID(t, e); after == was {
			t.Errorf("%s after the first Bind's end is on its mount %d still, and want it unmounted", e, after)
		}
		if !second.skipped || second.failed || !second.says(e+" was bound by another Bind first") {
			t.Errorf("a Bind whose mount stood on another's at %s skipped %v, failed %v, %q, and want it skipped saying the other was first", e, second.skipped, second.failed, second.said)
		}
		if now != was || err != nil || first.failed {
			t.Errorf("%s is on mount %d, and was on the first Bind's, %d; %s: %v; its end failed %v: %q; and want the first's mount standing, unchanged", e, now, was, dir, err, first.failed, first.said)
		}
	})
	t.Run("a Bind whose mount went off with the one beneath it", func(t *testing.T) {
		es := freeEntries(t, 2)
		first := call(t, func(r *recorder) { trusttest.BindOver(r, trusttest.Nosuid, es[:1]) })
		if first.failed || first.skipped {
			first.end()
			t.Fatalf("Bind failed %v, skipped %v: %q", first.failed, first.skipped, first.said)
		}
		was := mountID(t, es[0])
		var entry string
		ended := false
		second := call(t, func(r *recorder) {
			entry, _ = trusttest.BindHooked(r, trusttest.Nosuid, es, true, func() {
				if !ended { // the first's end, after the second's mount stood on its own
					ended = true
					first.end()
				}
			})
		})
		if second.failed || second.skipped || entry != es[1] {
			second.end()
			t.Fatalf("a Bind whose mount over %s went off with the first's gave %q, failed %v, skipped %v: %q, and want it to go on to %s", es[0], entry, second.failed, second.skipped, second.said, es[1])
		}
		bound := mountID(t, es[1])
		second.end()
		if second.failed || first.failed {
			t.Errorf("the Binds' ends failed %v and %v: %q, %q, and want no unmount error over a mount already off", first.failed, second.failed, first.said, second.said)
		}
		if a, b := mountID(t, es[0]), mountID(t, es[1]); a == was || b == bound {
			t.Errorf("after both ends %s is on mount %d, the first Bind's %d, and %s on %d, the second's %d, and want neither on its Bind's", es[0], a, was, es[1], b, bound)
		}
	})
}

// What it costs when this fails: a test binary whose temp folder is named with a \r, U+00A0 or
// another byte Go counts as space and the kernel does not escape reads another process's Bind's
// mount at /mnt as no Bind's, its root and mount point shifted, and two processes' Binds stack on
// one entry, which v0.9.1's notes say no longer happens; or a root with an escaped space is not read.
func TestABindsMountIsReadFromMountinfoSplitOnTheKernelsSpaceAlone(t *testing.T) {
	t.Parallel()
	const tail = " rw,nosuid,relatime shared:9 - ext4 /dev/sda1 rw\n"
	cases := []struct {
		name, root, point, e string
		bound                bool
	}{
		{"a temp folder named with U+00A0", "/tmp/a b/TestX/001/trusttest-bind", "/mnt", "/mnt", true},
		{"a temp folder named with \\r", "/tmp/a\rb/TestX/001/trusttest-bind", "/srv", "/srv", true},
		{"a temp folder named with a space, as the kernel escapes it", `/tmp/a\040b/TestX/001/trusttest-bind`, "/mnt", "/mnt", true},
		{"a mount point named with U+00A0 after the entry", "/tmp/TestX/001/trusttest-bind", "/mnt x", "/mnt", false},
		{"a root not a Bind's", "/tmp/a b/TestX/001/other", "/mnt", "/mnt", false},
	}
	for _, c := range cases {
		info := "22 1 259:2 / / rw,relatime shared:1 - ext4 /dev/sda1 rw\n501 22 259:2 " + c.root + " " + c.point + tail
		if got := trusttest.BoundIn(info, c.e); got != c.bound {
			t.Errorf("%s: a Bind's mount at %s is read %t, and want %t", c.name, c.e, got, c.bound)
		}
	}
}

// What it costs when this fails: a Bind on Linux 5.12 or later whose open_tree, mount_setattr or
// move_mount a seccomp profile refuses skips with words that do not say whether the kernel or the
// profile refused, so a run's skips are read as an old kernel's.
func TestASkipOfARefusedMountNamesItsErrno(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		err  error
		want string
	}{
		{unix.ENOSYS, "ENOSYS"},
		{errors.Join(unix.EPERM, nil), "EPERM"},
		{errors.Join(unix.EPERM, unix.EBADF), "EPERM"},
		{errors.New("not an errno"), "no errno"},
	} {
		if got := trusttest.ErrnoOf(c.err); got != c.want {
			t.Errorf("%v: names %q, and want %q", c.err, got, c.want)
		}
	}
}

// What it costs when this fails: a Bind takes for its own, or for another process's, a mount that
// is no Bind's, as one whose root is x-trusttest-bind or a folder below a trusttest-bind, or one
// below the entry or at a longer name, and skips an entry that is free; or it reads a Bind's mount
// whose temp folder is removed, or whose entry is named with a space, tab, newline or backslash,
// as no Bind's, and stacks its own on it.
func TestABindsMarkIsItsRootsLastElementAtTheEntryItself(t *testing.T) {
	t.Parallel()
	const tail = " rw,nosuid,relatime shared:9 - ext4 /dev/sda1 rw\n"
	cases := []struct {
		name, root, point, e string
		bound                bool
	}{
		{"a root whose folder is removed", "/tmp/TestX/001/trusttest-bind//deleted", "/mnt", "/mnt", true},
		{"a root that ends in the marker after other bytes", "/tmp/TestX/001/x-trusttest-bind", "/mnt", "/mnt", false},
		{"a root that is the marker and more", "/tmp/TestX/001/trusttest-bind-2", "/mnt", "/mnt", false},
		{"a root below a folder named as the marker", "/tmp/TestX/001/trusttest-bind/sub", "/mnt", "/mnt", false},
		{"an entry named with a space, a tab, a newline and a backslash", "/tmp/TestX/001/trusttest-bind", `/mnt/a\040b\011c\012d\134e`, "/mnt/a b\tc\nd\\e", true},
		{"a marked mount below the entry", "/tmp/TestX/001/trusttest-bind", "/mnt/sub", "/mnt", false},
		{"a marked mount at a name the entry is a prefix of", "/tmp/TestX/001/trusttest-bind", "/mntx", "/mnt", false},
	}
	for _, c := range cases {
		info := "22 1 259:2 / / rw,relatime shared:1 - ext4 /dev/sda1 rw\n501 22 259:2 " + c.root + " " + c.point + tail
		if got := trusttest.BoundIn(info, c.e); got != c.bound {
			t.Errorf("%s: a Bind's mount at %q is read %t, and want %t", c.name, c.e, got, c.bound)
		}
	}
}

// What it costs when this fails: a Bind's unmount at its test's end, or as it undoes a mount that
// lost its entry, never ends where its mount stands still, as one no unmount through its handle
// reaches, and the test binary hangs to its timeout in place of failing; or it stops before its
// mount is off.
func TestBindsUnmountStopsOnAMountThatStandsStill(t *testing.T) {
	t.Parallel()
	const (
		root  = "22 1 259:2 / / rw - ext4 /dev/sda1 rw\n502 22 0:5 / /proc rw - proc proc rw\n"
		bound = "501 22 259:2 /tmp/TestX/001/trusttest-bind /mnt rw,nosuid - ext4 /dev/sda1 rw\n"
	)
	for _, c := range []struct {
		name          string
		off, unmounts int // off: the unmount after which mountinfo no longer shows 501, or 0 for none
		fails         bool
	}{
		{"a mount off at the second unmount", 2, 2, false},
		{"a mount that stands still, unmounted once for each of mountinfo's three mounts and once more", 0, 4, true},
	} {
		fd, err := unix.Open("/dev/null", unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			t.Fatalf("open /dev/null: %v", err)
		}
		n := 0
		err = trusttest.DetachBy(fd, "501", func() error { n++; return nil }, func() string {
			switch {
			case n > 100:
				t.Errorf("%s: %d unmounts and on, so detach has no bound", c.name, n)
				return root // off, so this detach ends
			case c.off != 0 && n >= c.off:
				return root
			}
			return root + bound
		})
		if n != c.unmounts || (err != nil) != c.fails {
			t.Errorf("%s: detach unmounts %d times and gives %v, and want %d unmounts and an error %t", c.name, n, err, c.unmounts, c.fails)
		}
	}
}

// What it costs when this fails: a Bind whose statx of its copy fails, or gives no mount id, as a
// kernel before 5.8 does, places a mount it cannot find in mountinfo after, and reads it as lost
// or as another's.
func TestBindFailsAStatxThatGivesNoMountID(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		mask  uint32
		err   error
		want  string
		fails bool
	}{
		{"a statx that gives the mount id", unix.STATX_MNT_ID, nil, "501", false},
		{"a statx that gives no mount id", unix.STATX_BASIC_STATS, nil, "", true},
		{"a statx that fails", unix.STATX_MNT_ID, unix.EINVAL, "", true},
	} {
		got, err := trusttest.MountID(c.mask, 501, c.err)
		if got != c.want || (err != nil) != c.fails {
			t.Errorf("%s: the mount id is %q, with %v, and want %q and an error %t", c.name, got, err, c.want, c.fails)
		}
	}
}

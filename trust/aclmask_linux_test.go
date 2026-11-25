package trust

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// accessList is a POSIX access list as Linux stores it in system.posix_acl_access: this account
// read and write, the account 4242 read and write, the group read, the mask mask, others nothing.
func accessList(mask uint16) []byte {
	const undefined = 0xffffffff
	b := binary.LittleEndian.AppendUint32(nil, 2)
	for _, e := range []struct {
		tag, perm uint16
		id        uint32
	}{{0x01, 6, undefined}, {0x02, 6, 4242}, {0x04, 4, undefined}, {0x10, mask, undefined}, {0x20, 0, undefined}} {
		b = binary.LittleEndian.AppendUint16(b, e.tag)
		b = binary.LittleEndian.AppendUint16(b, e.perm)
		b = binary.LittleEndian.AppendUint32(b, e.id)
	}
	return b
}

// aclElsewhere is where aclDir looks when the test's own folder takes no access list: Linux's
// shared-memory tmpfs, then the system's temporary folders, which TMPDIR does not move.
func aclElsewhere() []string { return []string{"/dev/shm", "/tmp", "/var/tmp"} }

// aclDir is a folder of the test's on a file system that takes a POSIX access list. It is the
// test's own folder when that one takes a list. When its file system answers that it takes none
// (xfs and btrfs on Docker Desktop's kernel), it is a new folder in the first of
// aclElsewhere that passes the walk and takes a list, removed when the test ends. The rule under
// test reads the list, not the file system it is on. With no such folder the test fails, naming
// each place it tried and why; it is never skipped.
func aclDir(t *testing.T) string {
	t.Helper()
	d := realDir(t)
	err := takesList(d)
	if err == nil {
		return d
	}
	if !errors.Is(err, unix.ENOTSUP) {
		t.Fatalf("the test's folder %s: %v", d, err)
	}
	tried := []string{fmt.Sprintf("%s: %v", d, err)}
	for _, at := range aclElsewhere() {
		e, err := listDir(t, at)
		if err == nil {
			t.Logf("%s takes no access list, so the fixture is made in %s", d, e)
			return e
		}
		tried = append(tried, fmt.Sprintf("%s: %v", at, err))
	}
	t.Fatalf("no folder here takes a POSIX access list, and the test needs one: %s", strings.Join(tried, "; "))
	return ""
}

// listDir is a new folder of the test's in at, which passes the walk as realDir's does and takes
// an access list, or the reason it is not.
func listDir(t *testing.T, at string) (string, error) {
	t.Helper()
	d, err := os.MkdirTemp(at, "go-files-trust-acl-")
	if err != nil {
		return "", err
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(d); err != nil {
			t.Errorf("removing the fixture's folder: %v", err)
		}
	})
	if d, err = filepath.EvalSymlinks(d); err != nil {
		return "", err
	}
	if _, err := Path(d, Folder, Who{NoLinks: true}); err != nil {
		return "", fmt.Errorf("does not pass the walk: %w", err)
	}
	return d, takesList(d)
}

// takesList is nil when a file made in d takes an access list, or the error setting one gave.
func takesList(d string) error {
	p := filepath.Join(d, "probe")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		return err
	}
	serr := unix.Setxattr(p, "system.posix_acl_access", accessList(6), 0)
	if err := os.Remove(p); err != nil {
		return err
	}
	if serr != nil {
		return fmt.Errorf("setting an access list: %w", serr)
	}
	return nil
}

// What it costs when this fails: on Linux a file whose access list lets another account write it
// passes as a file only its group can write, and that account changes what the program reads.
func TestAFileWhoseAccessListMaskGrantsWriteIsRefusedOnLinux(t *testing.T) {
	t.Parallel()
	d := aclDir(t)
	p := filepath.Join(d, "f")
	mk(t, p, File, 0o640)
	if err := unix.Setxattr(p, "system.posix_acl_access", accessList(6), 0); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o660 {
		t.Fatalf("the mode is %v, and want the mask in its group bits, 0660", info.Mode().Perm())
	}
	gid := info.Sys().(*syscall.Stat_t).Gid
	ours := Who{Group: func(g uint32) bool { return g == gid }}
	_, err = Path(p, File, ours)
	u, ok := errors.AsType[*Unsafe](err)
	if !ok || !errors.Is(err, ErrOthersWrite) || u.Fix != (Fix{Path: p, Clear: 0o020}) {
		t.Fatalf("a mask that grants write: %v, and want ErrOthersWrite clearing its group's write", err)
	}
	applyFix(t, u.Fix, p)
	if _, err := Path(p, File, ours); err != nil {
		t.Errorf("after %+v, a read-only mask: %v", u.Fix, err)
	}
	if err := unix.Setxattr(p, "system.posix_acl_access", accessList(4), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := Path(p, File, Who{}); err != nil {
		t.Errorf("the list set again with a read-only mask: %v", err)
	}
	if n, err := look(p); err != nil || n.perm.Perm() != 0o640 {
		t.Errorf("the mode after a read-only mask: %v, %v, and want 0640", n.perm, err)
	}
}

// What it costs when this fails: a file held open whose access list lets another account write
// it passes Held as one only its group can write.
func TestAHeldFileWhoseAccessListMaskGrantsWriteIsRefused(t *testing.T) {
	t.Parallel()
	d := aclDir(t)
	p := filepath.Join(d, "f")
	mk(t, p, File, 0o640)
	if err := unix.Setxattr(p, "system.posix_acl_access", accessList(6), 0); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	gid := info.Sys().(*syscall.Stat_t).Gid
	ours := Who{Group: func(g uint32) bool { return g == gid }}
	err = Held(f, File, ours)
	if u, ok := errors.AsType[*Unsafe](err); !ok || !errors.Is(err, ErrOthersWrite) || u.Fix != (Fix{}) {
		t.Fatalf("a held file whose mask grants write: %v, and want ErrOthersWrite with no Fix, which is Path's to give", err)
	}
	_, err = Path(p, File, ours)
	if u, ok := errors.AsType[*Unsafe](err); !ok || !errors.Is(err, ErrOthersWrite) || u.Fix != (Fix{Path: p, Clear: 0o020}) {
		t.Fatalf("its name, by Path: %v, and want ErrOthersWrite clearing its group's write", err)
	}
	if err := unix.Setxattr(p, "system.posix_acl_access", accessList(4), 0); err != nil {
		t.Fatal(err)
	}
	if err := Held(f, File, ours); err != nil {
		t.Errorf("the list set again with a read-only mask: %v", err)
	}
}

package procfs

import (
	"errors"
	"io/fs"
	"os"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

// held is path's file system magic and inode, read by the test itself, so each row of Root's test
// is refused by the one check it names.
func held(t *testing.T, path string) (magic uint32, ino uint64) {
	t.Helper()
	fd, err := unix.Open(path, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	var sf unix.Statfs_t
	var st unix.Stat_t
	if err := unix.Fstatfs(fd, &sf); err != nil {
		t.Fatal(err)
	}
	if err := unix.Fstat(fd, &st); err != nil {
		t.Fatal(err)
	}
	return uint32(sf.Type), st.Ino
}

// What it costs when this fails: a tmpfs mounted over /proc, or a folder of a procfs, as a
// process's own, bound there, is read as procfs's root, and its links and files, which an owner of
// this mount namespace wrote, are taken for the kernel's: trust's mountinfo and confine's names.
func TestRootHoldsAProcfsByItsMagicAndItsRootInode(t *testing.T) {
	t.Parallel()
	pid := "/proc/" + strconv.Itoa(os.Getpid())
	if magic, ino := held(t, "/sys"); magic == unix.PROC_SUPER_MAGIC || ino != rootIno {
		t.Fatalf("/sys is magic %#x and inode %d, and the test wants another file system's root, inode %d, which only the magic refuses", magic, ino, rootIno)
	}
	if magic, ino := held(t, pid); magic != unix.PROC_SUPER_MAGIC || ino == rootIno {
		t.Fatalf("%s is magic %#x and inode %d, and the test wants a procfs folder not its root, which only the inode refuses", pid, magic, ino)
	}
	fd, err := Root("/proc")
	if err != nil {
		t.Fatalf("Root of /proc: %v", err)
	}
	unix.Close(fd)
	for _, row := range []struct{ path, why string }{
		{"/sys", "sysfs's root, by its magic"},
		{pid, "this process's procfs folder, not its root, by its inode"},
	} {
		fd, err := Root(row.path)
		var pe *fs.PathError
		if !errors.Is(err, ErrNotProc) || !errors.As(err, &pe) || pe.Op != "fstatfs" || pe.Path != row.path || fd != -1 {
			t.Errorf("Root of %s, %s: %d, %v, and want -1 and ErrNotProc in an *fs.PathError of fstatfs on it", row.path, row.why, fd, err)
		}
	}
}

// What it costs when this fails: Beneath follows a magic link of procfs, as self/cwd or another
// process's fd, out of /proc to a file an owner of this mount namespace chose.
func TestBeneathFollowsNoMagicLinkButFollowsSelf(t *testing.T) {
	t.Parallel()
	root, err := Root("/proc")
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(root)
	fd, err := Beneath(root, "self/fd", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC)
	if err != nil {
		t.Fatalf("Beneath /proc, self/fd: %v, and want it opened, through the plain link self", err)
	}
	unix.Close(fd)
	if fd, err := Beneath(root, "self/cwd", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC); !errors.Is(err, unix.ELOOP) {
		if err == nil {
			unix.Close(fd)
		}
		t.Errorf("Beneath /proc, self/cwd, a magic link: %v, and want ELOOP", err)
	}
}

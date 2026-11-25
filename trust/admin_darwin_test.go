package trust

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

// What it costs when this fails: on macOS a program that lets the admin group write its folders
// has no gid to name it by, or names another group, and refuses the folders an administrator
// installed or lets that other group in.
func TestAdminGroupOnMacOSIsTheGroupTheSystemNamesAdmin(t *testing.T) {
	t.Parallel()
	gid, ok, err := AdminGroup()
	if err != nil || !ok {
		t.Fatalf("AdminGroup: %d, %t, %v, and want the admin group", gid, ok, err)
	}
	g, err := user.LookupGroupId(strconv.FormatUint(uint64(gid), 10))
	if err != nil || g.Name != "admin" {
		t.Errorf("gid %d is the group %v (%v), and want admin", gid, g, err)
	}
}

// What it costs when this fails: AdminGroup's gid does not fit Who.Group, so a folder of the admin
// group that group may write is refused where the program lets that group in, or passes where it
// does not.
func TestAFolderTheAdminGroupCanWritePassesOnlyWhereWhoLetsTheAdminGroupIn(t *testing.T) {
	t.Parallel()
	gid, ok, err := AdminGroup()
	if err != nil || !ok {
		t.Fatalf("AdminGroup: %d, %t, %v, and want the admin group", gid, ok, err)
	}
	p := filepath.Join(realDir(t), "admins")
	mk(t, p, Folder, 0o775)
	if err := os.Lchown(p, -1, int(gid)); err != nil {
		t.Fatalf("this account cannot give %s to the admin group, gid %d, and the test needs it: %v", p, gid, err)
	}
	if err := os.Chmod(p, 0o775); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(p); err != nil || info.Sys().(*syscall.Stat_t).Gid != gid {
		t.Fatalf("%s is not the admin group's (%v)", p, err)
	}
	if _, err := Path(p, Folder, Who{Group: func(g uint32) bool { return ok && g == gid }}); err != nil {
		t.Errorf("the admin group let in: %v", err)
	}
	if _, err := Path(p, Folder, Who{}); !errors.Is(err, ErrOthersWrite) {
		t.Errorf("no group let in: %v, and want ErrOthersWrite", err)
	}
}

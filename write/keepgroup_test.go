package write

import (
	"crypto/sha256"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// groupOf answers the group of the file at path, and false where a file has none (Windows).
func groupOf(t *testing.T, path string) (int, bool) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	_, gid, ok := idsOf(fi)
	return gid, ok
}

// twoGroups makes the folders a and b in dir, each in its own group of this account's, setgid, so a
// file made in either is in that folder's group, as macOS gives a new file its folder's group and a
// setgid folder does on Linux. Where the account has one group, a file has none, or a new file does
// not take its folder's group, it logs why and answers false: a test here reaches no skip, which
// fails the run on every system (tests.admission.5), so the row shows nothing there, as
// otherGroup's do.
func twoGroups(t *testing.T, dir string) (a, b int, ok bool) {
	t.Helper()
	groups, err := os.Getgroups()
	if err != nil {
		t.Logf("this account's groups cannot be listed here, so the row shows nothing: %v", err)
		return 0, 0, false
	}
	var mine []int
	for _, g := range append([]int{os.Getegid()}, groups...) {
		if g >= 0 && (len(mine) == 0 || g != mine[0]) {
			mine = append(mine, g)
		}
		if len(mine) == 2 {
			break
		}
	}
	if len(mine) < 2 {
		t.Logf("this account is in the one group %v, and the row needs two, so it shows nothing", mine)
		return 0, 0, false
	}
	for i, sub := range []string{"a", "b"} {
		path := filepath.Join(dir, sub)
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(path, -1, mine[i]); err != nil {
			t.Logf("the folder %s cannot be given this account's group %d, so the row shows nothing: %v", sub, mine[i], err)
			return 0, 0, false
		}
		if err := os.Chmod(path, 0o700|fs.ModeSetgid); err != nil {
			t.Logf("the folder %s cannot be made setgid, so the row shows nothing: %v", sub, err)
			return 0, 0, false
		}
		probe := filepath.Join(path, "probe")
		put0(t, probe, "")
		if g, ok := groupOf(t, probe); !ok || g != mine[i] {
			t.Logf("a file made in %s is in the group %d (%v), not its folder's %d, so the row shows nothing", sub, g, ok, mine[i])
			return 0, 0, false
		}
		if err := os.Remove(probe); err != nil {
			t.Fatal(err)
		}
	}
	return mine[0], mine[1], true
}

// Keep, KeepIn, PutBack and PutBackIn keep a copy's group bits only where the new file is in its
// source's group: a file 0640 in group a copied into a folder of group b is 0600 there, 0644 is
// 0604, as others' bits are not touched, and a copy in a's own folder stays 0640. Nothing is
// chowned: the copy is in its folder's group. KeepIn and PutBackIn are run through one root over
// both folders and through two, one opened at each folder, and PutBackIn with Gone where no file
// is.
//
// What it costs when this fails: a copy kept in, or put back from, a folder of another group is
// readable by that group's members, who could not read the file it copies.
func TestACopyKeepsItsGroupBitsOnlyInItsSourcesGroup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ga, gb, ok := twoGroups(t, dir)
	if !ok {
		return
	}
	at := func(name string) string { return filepath.Join(dir, name) }
	for name, perm := range map[string]fs.FileMode{"a/file": 0o640, "a/open": 0o644, "b/kept": 0o640, "a/near": 0o640} {
		put0(t, at(name), name)
		if err := os.Chmod(at(name), perm); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"a/back", "a/backin", "a/samein", "a/backin2"} {
		put0(t, at(name), "over")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ra, err := os.OpenRoot(at("a"))
	if err != nil {
		t.Fatal(err)
	}
	defer ra.Close()
	rb, err := os.OpenRoot(at("b"))
	if err != nil {
		t.Fatal(err)
	}
	defer rb.Close()
	over := sha256.Sum256([]byte("over"))
	rows := []struct {
		form, made string
		group      int
		want       fs.FileMode
		do         func() error
	}{
		{"Keep", "b/k1", gb, 0o600, func() error { _, err := Keep(at("a/file"), at("b"), "k1"); return err }},
		{"Keep, others' bits", "b/k2", gb, 0o604, func() error { _, err := Keep(at("a/open"), at("b"), "k2"); return err }},
		{"KeepIn", "b/k3", gb, 0o600, func() error { return KeepIn(root, "a/file", root, "b/k3") }},
		{"KeepIn in the source's group", "a/k4", ga, 0o640, func() error { return KeepIn(root, "a/file", root, "a/k4") }},
		{"KeepIn, two roots", "b/k5", gb, 0o600, func() error { return KeepIn(ra, "file", rb, "k5") }},
		{"PutBack", "a/back", ga, 0o600, func() error { return PutBack(at("b/kept"), at("a/back"), over) }},
		{"PutBackIn", "a/backin", ga, 0o600, func() error { return PutBackIn(root, "b/kept", root, "a/backin", over) }},
		{"PutBackIn in the source's group", "a/samein", ga, 0o640, func() error { return PutBackIn(root, "a/near", root, "a/samein", over) }},
		{"PutBackIn, two roots", "a/backin2", ga, 0o600, func() error { return PutBackIn(rb, "kept", ra, "backin2", over) }},
		{"PutBackIn with Gone, two roots", "a/gone", ga, 0o600, func() error { return PutBackIn(rb, "kept", ra, "gone", [sha256.Size]byte{}, Gone()) }},
	}
	for _, r := range rows {
		if err := r.do(); err != nil {
			t.Errorf("%s: %v", r.form, err)
			continue
		}
		fi, err := os.Stat(at(r.made))
		if err != nil {
			t.Fatal(err)
		}
		g, _ := groupOf(t, at(r.made))
		if g != r.group || fi.Mode().Perm() != r.want {
			t.Errorf("%s: %s is %v in the group %d, and want %v in the group %d, its folder's", r.form, r.made, fi.Mode().Perm(), g, r.want, r.group)
		}
	}
}

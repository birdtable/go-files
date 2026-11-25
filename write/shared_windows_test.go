package write

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/sys/windows"
)

// usersSID is BUILTIN\Users, which every account of the machine is in.
const usersSID = "S-1-5-32-545"

// readableFolder makes the folder name in dir with a protected list granting SYSTEM and this account
// every right and BUILTIN\Users read, each to all the folder holds, as a service's shared config
// folder is, and answers its path.
func readableFolder(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	listed(t, path, "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;"+me(t)+")(A;OICI;FR;;;BU)")
	return path
}

// inherited fails t unless path has no list of its own: unprotected, every entry inherited from its
// folder, BUILTIN\Users' read among them, and not read-only.
func inherited(t *testing.T, path string) {
	t.Helper()
	_, protected, got := listOf(t, path)
	if protected || !slices.ContainsFunc(got, func(e entry) bool { return e.who == usersSID }) ||
		slices.ContainsFunc(got, func(e entry) bool { return e.flags&windows.INHERITED_ACE == 0 }) {
		t.Errorf("%s: protected %v, list %+v; want unprotected, every entry inherited, BUILTIN\\Users' among them", path, protected, got)
	}
	if isReadOnly(t, path) {
		t.Errorf("%s is read-only, and Shared does not make it so", path)
	}
}

// A file File writes with Shared has no list of its own: it holds its folder's inheritable entries
// alone, BUILTIN\Users' read among them, made and over a file there. A plain write in the same folder
// is still the owner-only list. On a volume that keeps no access lists a Shared write is taken and
// a plain one refused with ErrNoAccessList.
//
// What it costs when this fails: a service's config other accounts read is written owner-only and
// they cannot read it, or a secret written beside it takes the folder's grants, or a Shared write
// to a FAT stick is refused though there is no list it could give.
func TestASharedFileHoldsItsFoldersListAlone(t *testing.T) {
	t.Parallel()
	pub := readableFolder(t, t.TempDir(), "pub")
	conf := filepath.Join(pub, "app.toml")
	if err := File(conf, []byte("one"), Shared(), NoSync()); err != nil {
		t.Fatal(err)
	}
	inherited(t, conf)
	if err := File(conf, []byte("two"), Shared(), NoSync()); err != nil {
		t.Fatal(err)
	}
	inherited(t, conf)
	holds(t, conf, "two")
	if err := File(filepath.Join(pub, "key"), []byte("secret"), NoSync()); err != nil {
		t.Fatal(err)
	}
	ownerOnly(t, filepath.Join(pub, "key"))
	if err := File(filepath.Join(pub, "fat.toml"), []byte("x"), Shared(), NoSync(), listless()); err != nil {
		t.Errorf("a Shared write on a volume with no access lists: %v, and want it taken", err)
	}
	if err := File(filepath.Join(pub, "fat.key"), []byte("x"), NoSync(), listless()); !errors.Is(err, ErrNoAccessList) {
		t.Errorf("a plain write on a volume with no access lists: %v, and want ErrNoAccessList", err)
	}
	alone(t, pub, "app.toml", "fat.toml", "key")
}

// Over an old plain file, KeepMode or KeepOwner with Shared gives the new file the owner-only list,
// and its folder's list only where there is no old file. Over the file that first write made, whose
// folder's list grants Users read, KeepMode is refused with ErrNoMode, its rule unchanged by Shared,
// and nothing is written; KeepOwner takes it and gives the owner-only list. Keep and PutBack take
// Shared and give the owner-only list, and Keep is still refused with ErrNoAccessList on a volume
// that keeps none.
//
// What it costs when this fails: a file rewritten with KeepMode, meant to keep what it was, takes
// its folder's grants on the second write or is narrowed past who read it, or a copy of a secret
// kept to restore takes them.
func TestSharedUnderKeepModeKeepOwnerKeepAndPutBackOnWindows(t *testing.T) {
	t.Parallel()
	pub := readableFolder(t, t.TempDir(), "pub")
	for what, keep := range map[string]Option{"KeepMode": KeepMode(), "KeepOwner": KeepOwner()} {
		path := filepath.Join(pub, what)
		if err := File(path, []byte("one"), keep, Shared(), NoSync()); err != nil {
			t.Fatalf("%s, no old file: %v", what, err)
		}
		inherited(t, path)
		err := File(path, []byte("two"), keep, Shared(), NoSync())
		switch what {
		case "KeepMode":
			if !errors.Is(err, ErrNoMode) {
				t.Errorf("KeepMode, over the file its folder's list reaches: %v, and want ErrNoMode", err)
			}
			holds(t, path, "one")
			inherited(t, path)
		default:
			if err != nil {
				t.Fatalf("%s, over the file its folder's list reaches: %v", what, err)
			}
			ownerOnly(t, path)
		}
		plain := filepath.Join(pub, what+".plain")
		if err := File(plain, []byte("one"), NoSync()); err != nil {
			t.Fatal(err)
		}
		if err := File(plain, []byte("two"), keep, Shared(), NoSync()); err != nil {
			t.Fatalf("%s, over an old plain file: %v", what, err)
		}
		ownerOnly(t, plain)
		holds(t, plain, "two")
	}
	conf := filepath.Join(pub, "app.toml")
	if err := File(conf, []byte("one"), Shared(), NoSync()); err != nil {
		t.Fatal(err)
	}
	kept, err := Keep(conf, pub, "app.toml.kept", Shared(), NoSync())
	if err != nil {
		t.Fatal(err)
	}
	ownerOnly(t, kept)
	if err := PutBack(kept, conf, sha256.Sum256([]byte("one")), Shared(), NoSync()); err != nil {
		t.Fatal(err)
	}
	ownerOnly(t, conf)
	if _, err := Keep(conf, pub, "fat.kept", Shared(), NoSync(), listless()); !errors.Is(err, ErrNoAccessList) {
		t.Errorf("Keep with Shared on a volume with no access lists: %v, and want ErrNoAccessList", err)
	}
}

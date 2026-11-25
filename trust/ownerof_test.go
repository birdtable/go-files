package trust

import (
	"os"
	"path/filepath"
	"testing"
)

// OwnerOf reads the owner of an open folder, a root's included, and of an open file, in the form
// ReadOwned takes: this account's, or on Windows, where an elevated account's new folder is the
// Administrators group's, that group's. A closed file is an error, never an owner.
//
// What it costs when this fails: a program that checks who owns its folder
// gets a string ReadOwned refuses, or a wrong owner it then trusts.
func TestOwnerOfReadsTheOwnerOfAnOpenFolderAndFile(t *testing.T) {
	t.Parallel()
	mine, _ := Account()
	if mine == "" {
		t.Fatal("this account cannot be read, and every owner here is compared with it")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, name := range []string{".", "file"} {
		f, err := root.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		got, err := OwnerOf(f)
		f.Close()
		if err != nil || !ownerForm.MatchString(got) || got != mine && got != administrators {
			t.Errorf("OwnerOf %q: %q (%v), and want %q, this account", name, got, err, mine)
		}
		if got, err := OwnerOf(f); err == nil {
			t.Errorf("OwnerOf %q once closed: %q, and want an error", name, got)
		}
	}
}

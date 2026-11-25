package confine_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// A file with a second name, a hard link, is refused when it is opened to
// be written, by either name, and read as it is: a hard link to a file
// elsewhere in the root is that file, and no walk sees it. It runs on
// Windows too, where any account that may write a file may give it another
// name with no privilege.
//
// What it costs when this fails: a repository that commits a hard link to
// a file the program guards, made by a clone that keeps links, has a write
// to the link land in that file.
func TestAFileWithASecondNameIsNotOpenedToBeWritten(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "real", "config")
	if err := os.WriteFile(config, []byte("config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(config, filepath.Join(dir, "hard")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	tried := 0
	for _, name := range []string{"hard", filepath.Join("real", "config")} {
		for how, flag := range map[string]int{
			"append":  os.O_WRONLY | os.O_APPEND,
			"replace": os.O_RDWR | os.O_TRUNC,
		} {
			f, err := confine.Open(root, name, flag, 0)
			if f != nil {
				_ = f.Close()
			}
			refusedAt(t, "Open("+name+") to "+how, err, filepath.ToSlash(name))
			tried++
		}
		f, err := confine.Open(root, name, os.O_RDONLY, 0)
		if err != nil {
			t.Errorf("Open(%s) to read = %v, want it read", name, err)
			continue
		}
		_ = f.Close()
	}
	if tried == 0 {
		t.Fatal("no open was tried")
	}
	if got, err := os.ReadFile(config); err != nil || string(got) != "config\n" {
		t.Errorf("real/config holds %q, %v, want it as it was", got, err)
	}
}

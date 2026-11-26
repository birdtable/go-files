package write_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/birdtable/go-files/write"
)

// A program saves its settings whole: a reader sees the old file or the new one, never half. The
// folder is the program's own, made 0700 under a name no other account chose, so no other account
// can make the file first. The README shows this function as it is, and a test holds the two the
// same.
func ExampleFile() {
	dir, err := os.MkdirTemp("", "myprogram")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	settings := filepath.Join(dir, "settings.json")

	err = write.File(settings, []byte("{\"theme\": \"dark\"}\n"))
	if err != nil {
		log.Fatal(err)
	}
	b, err := os.ReadFile(settings)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(string(b))
	// Output: {"theme": "dark"}
}

// A program keeps a copy of a project's settings in its own state folder before an upgrade writes
// them, and puts the copy back when the upgrade fails, only over the bytes the upgrade wrote. The
// project and the state folder are two roots; every step goes through the root its name is
// beneath, so a folder on the way turned into a link out of either is refused.
func ExampleKeepIn() {
	projectDir, err := os.MkdirTemp("", "myproject")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(projectDir)
	stateDir, err := os.MkdirTemp("", "myprogram-state")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(stateDir)
	project, err := os.OpenRoot(projectDir)
	if err != nil {
		log.Fatal(err)
	}
	defer project.Close()
	state, err := os.OpenRoot(stateDir)
	if err != nil {
		log.Fatal(err)
	}
	defer state.Close()

	if err := write.FileIn(project, "settings.json", []byte("{\"theme\": \"dark\"}\n")); err != nil {
		log.Fatal(err)
	}
	if err := write.KeepIn(project, "settings.json", state, "settings.json.v1"); err != nil {
		log.Fatal(err)
	}
	upgraded := []byte("{\"theme\": \"light\"}\n")
	if err := write.FileIn(project, "settings.json", upgraded); err != nil {
		log.Fatal(err)
	}
	if err := write.PutBackIn(state, "settings.json.v1", project, "settings.json", sha256.Sum256(upgraded)); err != nil {
		log.Fatal(err)
	}
	b, err := project.ReadFile("settings.json")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(string(b))
	// Output: {"theme": "dark"}
}

// A program logs through log/slog, one JSON record a line, to a log beneath its state folder
// kept under 1 MiB, the full log moved to inventory.log.1. The handler writes each record with
// its newline in one Write, which LogWriter appends as one line. The time is left out here so
// the output is the same at every run.
func ExampleLogWriter() {
	stateDir, err := os.MkdirTemp("", "myprogram-state")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(stateDir)
	state, err := os.OpenRoot(stateDir)
	if err != nil {
		log.Fatal(err)
	}
	defer state.Close()

	w := write.LogWriter(context.Background(), state, "inventory.log", 1<<20, write.Aside("inventory.log.1"))
	noTime := func(groups []string, a slog.Attr) slog.Attr {
		if len(groups) == 0 && a.Key == slog.TimeKey {
			return slog.Attr{}
		}
		return a
	}
	logger := slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{ReplaceAttr: noTime}))
	logger.Info("counted", "shelf", "a3", "items", 12)
	logger.Warn("short", "shelf", "b1", "want", 4, "have", 1)

	b, err := state.ReadFile("inventory.log")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(string(b))
	// Output:
	// {"level":"INFO","msg":"counted","shelf":"a3","items":12}
	// {"level":"WARN","msg":"short","shelf":"b1","want":4,"have":1}
}

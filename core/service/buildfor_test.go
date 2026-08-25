package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// writeGoModule seeds the smallest module validateSource accepts. Cross-
// compiling is what is under test, not herrscher itself.
func writeGoModule(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/Herrscherd/herrscher\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildForCrossCompiles(t *testing.T) {
	src := t.TempDir()
	writeGoModule(t, src)
	out := filepath.Join(t.TempDir(), "hs")
	if err := BuildFor(context.Background(), src, out, "linux", "arm64"); err != nil {
		t.Fatalf("cross build: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("no binary produced: %v", err)
	}
}

func TestBuildForRefusesSomethingThatIsNotTheSource(t *testing.T) {
	if err := BuildFor(context.Background(), t.TempDir(), filepath.Join(t.TempDir(), "hs"), "linux", "amd64"); err == nil {
		t.Fatal("want a refusal for a directory that is not a herrscher checkout")
	}
}

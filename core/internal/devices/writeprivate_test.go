package devices

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWritePrivateLeavesOnlyTheTargetBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "authority-key.pem")
	if err := writePrivate(path, []byte("secret")); err != nil {
		t.Fatalf("writePrivate: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "authority-key.pem" {
		t.Fatalf("directory after a write holds %d entries, want only the target", len(entries))
	}
}

func TestWritePrivateRemovesItsTemporaryFileWhenTheRenameFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "authority-key.pem")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	err := writePrivate(path, []byte("secret"))
	if !errors.Is(err, ErrAuthorityFiles) {
		t.Fatalf("writePrivate over a directory = %v, want ErrAuthorityFiles", err)
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("ReadDir: %v", readErr)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("directory after a failed rename holds %d entries, want only the directory", len(entries))
	}
}

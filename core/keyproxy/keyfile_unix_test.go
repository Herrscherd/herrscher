//go:build unix

package keyproxy

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/Herrscherd/herrscher/core/config"
)

func TestKeyFilePipeIsRefusedWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKeyFile(t, path); !errors.Is(err, errKeyFileNotRegular) {
		t.Fatalf("pipe: err %v, want errKeyFileNotRegular", err)
	}
}

func TestKeyFileSymlinkIsRefused(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	if err := os.WriteFile(target, []byte("sk-linked"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKeyFile(t, link); !errors.Is(err, syscall.ELOOP) {
		t.Fatalf("symlink: err %v, want ELOOP from O_NOFOLLOW", err)
	}
}

func TestKeyFileIsReadThroughTheOpenedDescriptor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	if err := os.WriteFile(path, []byte("sk-original"), 0o600); err != nil {
		t.Fatal(err)
	}
	swapped := filepath.Join(dir, "swapped")
	if err := os.WriteFile(swapped, []byte("sk-swapped"), 0o600); err != nil {
		t.Fatal(err)
	}
	loader := fileLoader()
	loader.OpenFile = func(name string) (*os.File, error) {
		file, openErr := openKeyFile(name)
		if openErr != nil {
			return nil, openErr
		}
		if removeErr := os.Remove(name); removeErr != nil {
			return nil, removeErr
		}
		if linkErr := os.Symlink(swapped, name); linkErr != nil {
			return nil, linkErr
		}
		return file, nil
	}
	secret, err := loader.Load(t.Context(), config.KeySource{Name: "file", KeyFile: path})
	if err != nil || secret.Value() != "sk-original" {
		t.Fatalf("value %q err %v, want the key opened before the swap", secret.Value(), err)
	}
}

func TestKeyFileBehindALinkedDirectoryIsRefused(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "key"), []byte("sk-behind"), 0o600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(dir, "linked")
	if err := os.Symlink(realDir, linked); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKeyFile(t, filepath.Join(linked, "key")); !errors.Is(err, errKeyFileLinkedDir) {
		t.Fatalf("linked directory: err %v, want errKeyFileLinkedDir", err)
	}
}

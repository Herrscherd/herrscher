//go:build unix

package filelock

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenRefusesASymbolicLink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	link := filepath.Join(dir, ".lock")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	file, err := Open(link)
	if !errors.Is(err, ErrSymlink) {
		if file != nil {
			closeFile(file)
		}
		t.Fatalf("Open of a symbolic link = %v, want ErrSymlink", err)
	}
}

func TestCheckOwnerRefusesAFileOfAnotherUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".lock")
	file, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer closeFile(file)
	if err := checkOwner(file, os.Getuid()); err != nil {
		t.Fatalf("checkOwner for the owner = %v, want nil", err)
	}
	if err := checkOwner(file, os.Getuid()+1); !errors.Is(err, ErrNotOwned) {
		t.Fatalf("checkOwner for another user = %v, want ErrNotOwned", err)
	}
}

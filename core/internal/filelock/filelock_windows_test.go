//go:build windows

package filelock

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestOpenRefusesAJunction(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	link := filepath.Join(dir, ".lock")
	if out, err := exec.CommandContext(t.Context(), "cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v: %s", err, out)
	}
	file, err := Open(link)
	if !errors.Is(err, ErrSymlink) {
		if file != nil {
			closeFile(file)
		}
		t.Fatalf("Open of a junction = %v, want ErrSymlink", err)
	}
}

func TestOpenRefusesASymbolicLink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	link := filepath.Join(dir, ".lock")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links need a privilege this session lacks: %v", err)
	}
	file, err := Open(link)
	if !errors.Is(err, ErrSymlink) {
		if file != nil {
			closeFile(file)
		}
		t.Fatalf("Open of a symbolic link = %v, want ErrSymlink", err)
	}
}

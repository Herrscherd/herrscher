//go:build unix

package keyproxy

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
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

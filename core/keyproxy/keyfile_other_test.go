//go:build !unix

package keyproxy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyFileIsRefusedOnThisPlatform(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte("sk-plain"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadKeyFile(t, path)
	if !errors.Is(err, errKeyFileUnsupported) {
		t.Fatalf("key_file on this platform: err %v, want errKeyFileUnsupported", err)
	}
	if !strings.Contains(err.Error(), "key_env") || !strings.Contains(err.Error(), "key_cmd") {
		t.Fatalf("refusal %q does not point to key_env or key_cmd", err)
	}
}

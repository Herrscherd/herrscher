package keyproxy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/config"
)

func fileLoader() Loader {
	return Loader{Getenv: os.Getenv, Lstat: os.Lstat, OpenFile: openKeyFile, Home: os.UserHomeDir}
}

func loadKeyFile(t *testing.T, path string) (Secret, error) {
	t.Helper()
	return fileLoader().Load(t.Context(), config.KeySource{Name: "file", KeyFile: path})
}

func TestKeyFileOwnerOnlyIsAccepted(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o400} {
		path := filepath.Join(t.TempDir(), "key")
		if err := os.WriteFile(path, []byte("sk-owner\n"), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		secret, err := loadKeyFile(t, path)
		if err != nil || secret.Value() != "sk-owner" {
			t.Fatalf("mode %o: value %q err %v, want the key", mode, secret.Value(), err)
		}
	}
}

func TestKeyFileReadableByOthersIsRefused(t *testing.T) {
	for _, mode := range []os.FileMode{0o640, 0o604, 0o644} {
		path := filepath.Join(t.TempDir(), "key")
		if err := os.WriteFile(path, []byte("sk-open"), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := loadKeyFile(t, path); !errors.Is(err, ErrKeyFileOpen) {
			t.Fatalf("mode %o: err %v, want ErrKeyFileOpen", mode, err)
		}
	}
}

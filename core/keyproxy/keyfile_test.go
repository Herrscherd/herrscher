package keyproxy

import (
	"os"
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

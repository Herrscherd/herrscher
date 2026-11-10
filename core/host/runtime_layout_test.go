package host

import (
	"os"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

func makeShortRoot() (string, error) {
	return os.MkdirTemp(os.TempDir(), "hs-rt-")
}

func isolatedRuntimeDir(t *testing.T) runtimedir.Layout {
	t.Helper()
	dir, err := makeShortRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if removeErr := os.RemoveAll(dir); removeErr != nil {
			t.Error(removeErr)
		}
	})
	layout, err := runtimedir.At(dir)
	if err != nil {
		t.Fatal(err)
	}
	return layout
}

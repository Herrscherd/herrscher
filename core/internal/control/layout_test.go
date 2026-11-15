package control

import (
	"os"
	"path"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

func makeShortRoot() (string, error) {
	return os.MkdirTemp("/tmp", "rt")
}

func useShortRuntimeRoot(t *testing.T) runtimedir.Layout {
	t.Helper()
	root, err := makeShortRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if removeErr := os.RemoveAll(root); removeErr != nil {
			t.Error(removeErr)
		}
	})
	layout, err := runtimedir.At(path.Join(root, "h"))
	if err != nil {
		t.Fatal(err)
	}
	return layout
}

package supervisor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

func makeLayoutRoot() (string, error) {
	return os.MkdirTemp("/tmp", "sv")
}

func testLayout(t *testing.T) runtimedir.Layout {
	t.Helper()
	root, err := makeLayoutRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if removeErr := os.RemoveAll(root); removeErr != nil {
			t.Error(removeErr)
		}
	})
	layout, err := runtimedir.At(filepath.Join(root, "h"))
	if err != nil {
		t.Fatal(err)
	}
	return layout
}

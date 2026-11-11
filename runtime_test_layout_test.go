package main

import (
	"os"
	"testing"

	"github.com/Herrscherd/herrscher/core/host"
)

func makeShortRoot() (string, error) {
	return os.MkdirTemp(os.TempDir(), "hs-rt-")
}

func testRuntime(t *testing.T) host.RuntimeLayout {
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
	layout, err := host.RuntimeAt(root)
	if err != nil {
		t.Fatal(err)
	}
	return layout
}

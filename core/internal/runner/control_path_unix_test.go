//go:build unix

package runner

import (
	"os"
	"path"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

func TestControlPathIsStableAndCreatesItsDirectory(t *testing.T) {
	layout := shortLayout(t)
	a, err := ControlPathFor(layout, "me@build1")
	if err != nil {
		t.Fatalf("ControlPathFor: %v", err)
	}
	if b, err := ControlPathFor(layout, "me@build1"); err != nil || a != b {
		t.Fatalf("ControlPathFor is not stable: %q, %v", b, err)
	}
	if other, err := ControlPathFor(layout, "me@build2"); err != nil || a == other {
		t.Fatalf("two targets share one control socket: %q, %v", other, err)
	}
	if _, err := os.Stat(layout.Dir()); err != nil {
		t.Fatalf("the private directory was not created before ssh needs it: %v", err)
	}
}

func TestControlPathFailsWhenThePrivateDirectoryIsUnsafe(t *testing.T) {
	layout := shortLayout(t)
	if err := os.Mkdir(layout.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := ControlPathFor(layout, "me@build1"); err == nil || got != "" {
		t.Fatalf("ControlPathFor = %q, %v, want an error and no path so the caller runs without multiplexing", got, err)
	}
}

func makeShortRoot() (string, error) {
	return os.MkdirTemp("/tmp", "rn")
}

func shortLayout(t *testing.T) runtimedir.Layout {
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

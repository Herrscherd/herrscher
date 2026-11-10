//go:build unix

package runner

import (
	"os"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

func TestControlPathIsStableAndCreatesItsDirectory(t *testing.T) {
	layout := runtimedir.Layout{Root: t.TempDir()}
	a := controlPathIn(layout, "me@build1")
	if a == "" {
		t.Fatal("the control path is empty although the private directory is usable")
	}
	if a != controlPathIn(layout, "me@build1") {
		t.Fatal("controlPathIn is not stable")
	}
	if a == controlPathIn(layout, "me@build2") {
		t.Fatal("two targets share one control socket")
	}
	if _, err := os.Stat(layout.Dir()); err != nil {
		t.Fatalf("the private directory was not created before ssh needs it: %v", err)
	}
}

func TestControlPathIsEmptyWhenThePrivateDirectoryIsUnsafe(t *testing.T) {
	layout := runtimedir.Layout{Root: t.TempDir()}
	if err := os.Mkdir(layout.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := controlPathIn(layout, "me@build1"); got != "" {
		t.Fatalf("control path = %q, want none so ssh runs without multiplexing", got)
	}
}

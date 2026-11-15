//go:build !windows

package host

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

func TestCommandSocketPathUnix(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	if got := CommandSocketPathIn(layout, ""); got != filepath.Join(layout.Dir(), "herrscher-command.sock") {
		t.Fatalf("CommandSocketPathIn(\"\") = %q", got)
	}
	if got := CommandSocketPathIn(layout, "instance-1"); got != filepath.Join(layout.Dir(), "herrscher-command-instance-1.sock") {
		t.Fatalf("CommandSocketPathIn(instance-1) = %q", got)
	}
}

func TestEventsSocketPathIsTheDocumentedLocation(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	if got := EventsSocketPathIn(layout, ""); got != filepath.Join(layout.Dir(), "herrscher-events.sock") {
		t.Fatalf("EventsSocketPathIn(\"\") = %q", got)
	}
	if got := EventsSocketPathIn(layout, "instance-1"); got != filepath.Join(layout.Dir(), "herrscher-events-instance-1.sock") {
		t.Fatalf("EventsSocketPathIn(instance-1) = %q", got)
	}
}

func TestPublicSocketPathsShareOneDirectory(t *testing.T) {
	command := CommandSocketPath("instance-1")
	events := EventsSocketPath("instance-1")
	if filepath.Dir(command) != filepath.Dir(events) {
		t.Fatalf("CommandSocketPath and EventsSocketPath live in %q and %q", filepath.Dir(command), filepath.Dir(events))
	}
	if filepath.Base(command) != "herrscher-command-instance-1.sock" {
		t.Fatalf("CommandSocketPath(instance-1) base = %q", filepath.Base(command))
	}
	if filepath.Base(events) != "herrscher-events-instance-1.sock" {
		t.Fatalf("EventsSocketPath(instance-1) base = %q", filepath.Base(events))
	}
}

func TestOperatorSocketsFitTheUnixLimit(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	instance := strings.Repeat("i", 40)
	for _, path := range []string{
		CommandSocketPathIn(layout, instance),
		EventsSocketPathIn(layout, instance),
		SessionCommandSocketPathIn(layout, instance, strings.Repeat("s", 80)),
	} {
		if len(path) > runtimedir.MaxUnixSocketPath {
			t.Fatalf("socket %q is %d bytes, over the %d sun_path holds", path, len(path), runtimedir.MaxUnixSocketPath)
		}
	}
}

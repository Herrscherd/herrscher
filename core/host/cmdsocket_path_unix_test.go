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
	if got := commandSocketPathIn(layout, ""); got != filepath.Join(layout.Dir(), "herrscher-command.sock") {
		t.Fatalf("commandSocketPathIn(\"\") = %q", got)
	}
	if got := commandSocketPathIn(layout, "instance-1"); got != filepath.Join(layout.Dir(), "herrscher-command-instance-1.sock") {
		t.Fatalf("commandSocketPathIn(instance-1) = %q", got)
	}
}

func TestEventsSocketPathIsTheDocumentedLocation(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	if got := eventsSocketPathIn(layout, ""); got != filepath.Join(layout.Dir(), "herrscher-events.sock") {
		t.Fatalf("eventsSocketPathIn(\"\") = %q", got)
	}
	if got := eventsSocketPathIn(layout, "instance-1"); got != filepath.Join(layout.Dir(), "herrscher-events-instance-1.sock") {
		t.Fatalf("eventsSocketPathIn(instance-1) = %q", got)
	}
}

func TestPublicSocketPathsLiveInTheProductionDir(t *testing.T) {
	dir := runtimedir.Production().Dir()
	if got := CommandSocketPath("instance-1"); got != filepath.Join(dir, "herrscher-command-instance-1.sock") {
		t.Fatalf("CommandSocketPath(instance-1) = %q", got)
	}
	if got := EventsSocketPath("instance-1"); got != filepath.Join(dir, "herrscher-events-instance-1.sock") {
		t.Fatalf("EventsSocketPath(instance-1) = %q", got)
	}
}

func TestOperatorSocketsFitTheUnixLimit(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	instance := strings.Repeat("i", 40)
	for _, path := range []string{
		commandSocketPathIn(layout, instance),
		eventsSocketPathIn(layout, instance),
		sessionCommandSocketPathIn(layout, instance, strings.Repeat("s", 80)),
	} {
		if len(path) > runtimedir.MaxUnixSocketPath {
			t.Fatalf("socket %q is %d bytes, over the %d sun_path holds", path, len(path), runtimedir.MaxUnixSocketPath)
		}
	}
}

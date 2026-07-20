//go:build !windows

package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandSocketPathUnix(t *testing.T) {
	if got := CommandSocketPath(""); got != filepath.Join(os.TempDir(), "herrscher-command.sock") {
		t.Fatalf("CommandSocketPath(\"\") = %q", got)
	}
	got := CommandSocketPath("instance-1")
	if got != filepath.Join(os.TempDir(), "herrscher-command-instance-1.sock") || !strings.HasSuffix(got, ".sock") {
		t.Fatalf("CommandSocketPath(instance-1) = %q", got)
	}
}

func TestEventsSocketPathUnix(t *testing.T) {
	// Must equal the command socket with herrscher-command → herrscher-events, since
	// Neublox derives the events path from the command path exactly this way.
	if got := EventsSocketPath(""); got != filepath.Join(os.TempDir(), "herrscher-events.sock") {
		t.Fatalf("EventsSocketPath(\"\") = %q", got)
	}
	got := EventsSocketPath("instance-1")
	if got != filepath.Join(os.TempDir(), "herrscher-events-instance-1.sock") || !strings.HasSuffix(got, ".sock") {
		t.Fatalf("EventsSocketPath(instance-1) = %q", got)
	}
}

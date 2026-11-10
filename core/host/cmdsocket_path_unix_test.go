//go:build !windows

package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/control"
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
	if got := EventsSocketPath(""); got != filepath.Join(os.TempDir(), "herrscher-events.sock") {
		t.Fatalf("EventsSocketPath(\"\") = %q", got)
	}
	got := EventsSocketPath("instance-1")
	if got != filepath.Join(os.TempDir(), "herrscher-events-instance-1.sock") || !strings.HasSuffix(got, ".sock") {
		t.Fatalf("EventsSocketPath(instance-1) = %q", got)
	}
}

func TestOperatorSocketsFitTheUnixLimitUnderALongTempDir(t *testing.T) {
	isolatedRuntimeDir(t)
	t.Setenv("TMPDIR", "/tmp/"+strings.Repeat("t", 60))
	instance := strings.Repeat("i", 40)
	for _, path := range []string{CommandSocketPath(instance), EventsSocketPath(instance), SessionCommandSocketPath(instance, strings.Repeat("s", 80))} {
		if len(path) > control.MaxUnixSocketPath {
			t.Fatalf("socket %q is %d bytes, over the %d sun_path holds", path, len(path), control.MaxUnixSocketPath)
		}
	}
}

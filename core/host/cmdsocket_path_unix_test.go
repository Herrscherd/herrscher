//go:build !windows

package host

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

func TestCommandSocketPathUnix(t *testing.T) {
	useProductionLayout(t)
	dir := runtimedir.Production().Dir()
	if got := CommandSocketPath(""); got != filepath.Join(dir, "herrscher-command.sock") {
		t.Fatalf("CommandSocketPath(\"\") = %q", got)
	}
	if got := CommandSocketPath("instance-1"); got != filepath.Join(dir, "herrscher-command-instance-1.sock") {
		t.Fatalf("CommandSocketPath(instance-1) = %q", got)
	}
}

func TestEventsSocketPathIsTheDocumentedLocation(t *testing.T) {
	useProductionLayout(t)
	dir := filepath.Join(runtimedir.Production().Root, "herrscher-"+strconv.Itoa(os.Getuid()))
	if got := EventsSocketPath(""); got != filepath.Join(dir, "herrscher-events.sock") {
		t.Fatalf("EventsSocketPath(\"\") = %q", got)
	}
	if got := EventsSocketPath("instance-1"); got != filepath.Join(dir, "herrscher-events-instance-1.sock") {
		t.Fatalf("EventsSocketPath(instance-1) = %q", got)
	}
}

func TestOperatorSocketsFitTheUnixLimit(t *testing.T) {
	isolatedRuntimeDir(t)
	instance := strings.Repeat("i", 40)
	for _, path := range []string{CommandSocketPath(instance), EventsSocketPath(instance), SessionCommandSocketPath(instance, strings.Repeat("s", 80))} {
		if len(path) > runtimedir.MaxUnixSocketPath {
			t.Fatalf("socket %q is %d bytes, over the %d sun_path holds", path, len(path), runtimedir.MaxUnixSocketPath)
		}
	}
}

func useProductionLayout(t *testing.T) {
	t.Helper()
	previous := runtimeLayout
	runtimeLayout = runtimedir.Production()
	t.Cleanup(func() { runtimeLayout = previous })
}

package host

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/control"
)

// On the daemon's own machine nothing sets the variable, and the historical
// path must be what a client dials.
func TestCommandSocketTargetDefaultsToTheInstancePath(t *testing.T) {
	t.Setenv(control.CommandSocketVar, "")
	if got, want := commandSocketTarget("inst"), CommandSocketPath("inst"); got != want {
		t.Fatalf("commandSocketTarget = %q, want %q", got, want)
	}
}

// On a machine carrying a remote session, the forwarded path is the only one
// that exists: a client that derived its own would dial nothing.
func TestCommandSocketTargetPrefersTheForwardedPath(t *testing.T) {
	t.Setenv(control.CommandSocketVar, " /tmp/herrscher-command-s1.sock ")
	if got := commandSocketTarget("inst"); got != "/tmp/herrscher-command-s1.sock" {
		t.Fatalf("commandSocketTarget = %q, want the forwarded path", got)
	}
}

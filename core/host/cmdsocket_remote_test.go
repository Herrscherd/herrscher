package host

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/control"
)

func TestCommandSocketTargetDefaultsToTheInstancePath(t *testing.T) {
	t.Setenv(control.CommandSocketVar, "")
	layout := testRuntime(t)
	if got, want := commandSocketTarget(layout, "inst"), CommandSocketPathIn(layout, "inst"); got != want {
		t.Fatalf("commandSocketTarget = %q, want %q", got, want)
	}
}

func TestCommandSocketTargetPrefersTheForwardedPath(t *testing.T) {
	t.Setenv(control.CommandSocketVar, " /tmp/herrscher-command-s1.sock ")
	if got := commandSocketTarget(testRuntime(t), "inst"); got != "/tmp/herrscher-command-s1.sock" {
		t.Fatalf("commandSocketTarget = %q, want the forwarded path", got)
	}
}

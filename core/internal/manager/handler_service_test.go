package manager

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/cli"
)

func TestServiceRestart(t *testing.T) {
	h, up, _, _, _, _, _ := newTestHandlerWithUpdater(t, "")
	out, err := cli.Plain(h.serviceRestartRun(context.Background(), args()))
	if err != nil {
		t.Fatal(err)
	}
	if up.restarts != 1 {
		t.Fatalf("expected 1 restart, got %d", up.restarts)
	}
	if len(up.builds) != 0 {
		t.Fatalf("restart must not build, got %+v", up.builds)
	}
	if !strings.Contains(out, "restarting the daemon") {
		t.Fatalf("unexpected reply: %q", out)
	}
}

func TestServiceUpdatePullsBuildsRestarts(t *testing.T) {
	h, up, _, _, _, _, _ := newTestHandlerWithUpdater(t, "")
	out, err := cli.Plain(h.serviceUpdateRun(context.Background(), args()))
	if err != nil {
		t.Fatal(err)
	}
	if len(up.builds) != 1 || up.builds[0] != true {
		t.Fatalf("expected one build with pull=true, got %+v", up.builds)
	}
	if up.restarts != 1 {
		t.Fatalf("expected restart after build, got %d", up.restarts)
	}
	if !strings.Contains(out, "abc1234") {
		t.Fatalf("reply should mention version: %q", out)
	}
}

func TestServiceUpdateNoPull(t *testing.T) {
	h, up, _, _, _, _, _ := newTestHandlerWithUpdater(t, "")
	if _, err := h.serviceUpdateRun(context.Background(), args("no_pull", "true")); err != nil {
		t.Fatal(err)
	}
	if len(up.builds) != 1 || up.builds[0] != false {
		t.Fatalf("expected build with pull=false, got %+v", up.builds)
	}
}

func TestServiceUpdateBuildFailsNoRestart(t *testing.T) {
	h, up, _, _, _, _, _ := newTestHandlerWithUpdater(t, "")
	up.buildErr = errors.New("compile boom")
	if _, err := h.serviceUpdateRun(context.Background(), args()); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected build error surfaced, got %v", err)
	}
	if up.restarts != 0 {
		t.Fatalf("must not restart when build fails")
	}
}

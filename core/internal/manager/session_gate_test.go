package manager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gateHandler(t *testing.T, grain, why string) (*Handler, string) {
	t.Helper()
	h, _, localWT, _ := handlerWithHosts(t)
	dir := t.TempDir()
	localWT.path = dir
	h.SetGateResolver(func(string) (string, string) { return grain, why })
	return h, dir
}

func TestSessionCreateGatesASessionWithoutAnAgent(t *testing.T) {
	h, dir := gateHandler(t, "tool", "")
	out, err := h.sessionCreateRun(context.Background(), args("name", "demo", "approvals", "strict"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if strings.Contains(out, "ungated") {
		t.Fatalf("warned about a session it did gate: %q", out)
	}
	buf, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("no hook materialized: %v", err)
	}
	if !strings.Contains(string(buf), "approve hook") {
		t.Fatalf("settings.local.json carries no hook: %s", buf)
	}
}

func TestSessionCreateLeavesATrackedSettingsFileAlone(t *testing.T) {
	h, dir := gateHandler(t, "tool", "")
	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	tracked := filepath.Join(claude, "settings.json")
	if err := os.WriteFile(tracked, []byte(`{"model":"repo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "approvals", "ask")); err != nil {
		t.Fatalf("create: %v", err)
	}
	buf, err := os.ReadFile(tracked)
	if err != nil {
		t.Fatalf("the tracked settings.json disappeared: %v", err)
	}
	if string(buf) != `{"model":"repo"}` {
		t.Fatalf("the tracked settings.json was rewritten: %s", buf)
	}
}

func TestSessionCreateWarnsWhenTheVendorCannotEnforce(t *testing.T) {
	h, dir := gateHandler(t, "", "cursor-agent exposes no permission hook")
	out, err := h.sessionCreateRun(context.Background(), args("name", "demo", "vendor", "cursor", "approvals", "strict"))
	if err != nil {
		t.Fatalf("create must not refuse: %v", err)
	}
	if !strings.Contains(out, "cursor-agent exposes no permission hook") {
		t.Fatalf("the warning must carry the plugin's reason, got %q", out)
	}
	if !strings.Contains(out, "ungated") {
		t.Fatalf("the warning must say the session is ungated, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("nothing may be materialized for a vendor that cannot enforce, err = %v", err)
	}
}

func TestSessionCreateWarnsOnASharedSession(t *testing.T) {
	h, _ := gateHandler(t, "tool", "")
	out, err := h.sessionCreateRun(context.Background(), args("name", "demo", "approvals", "strict", "shared", "true"))
	if err != nil {
		t.Fatalf("create must not refuse: %v", err)
	}
	if !strings.Contains(out, "no isolated worktree") {
		t.Fatalf("got %q, want a warning about the missing worktree", out)
	}
}

func TestSessionCreateWarnsOnARemoteSessionWithoutAnAgent(t *testing.T) {
	h, _ := gateHandler(t, "tool", "")
	out, err := h.sessionCreateRun(context.Background(), args("name", "demo", "host", "build1", "approvals", "strict"))
	if err != nil {
		t.Fatalf("create must not refuse: %v", err)
	}
	if !strings.Contains(out, "no materialization channel") {
		t.Fatalf("got %q, want a warning about the remote host", out)
	}
}

func TestSessionCreateStaysQuietAndUngatedOnBypass(t *testing.T) {
	h, dir := gateHandler(t, "tool", "")
	for _, mode := range []string{"", "bypass"} {
		out, err := h.sessionCreateRun(context.Background(), args("name", "demo"+mode, "approvals", mode))
		if err != nil {
			t.Fatalf("create %q: %v", mode, err)
		}
		if strings.Contains(out, "ungated") {
			t.Fatalf("mode %q warned about nothing, got %q", mode, out)
		}
		if _, err := os.Stat(filepath.Join(dir, ".claude")); !os.IsNotExist(err) {
			t.Fatalf("mode %q materialized a hook, err = %v", mode, err)
		}
	}
}

func TestSessionCreateWithNoResolverWarnsRatherThanAssumes(t *testing.T) {
	h, _, _, _ := handlerWithHosts(t)
	out, err := h.sessionCreateRun(context.Background(), args("name", "demo", "approvals", "ask"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.Contains(out, "ungated") {
		t.Fatalf("got %q, want a warning", out)
	}
}

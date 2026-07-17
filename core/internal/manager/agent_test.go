package manager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestAgentCreateAndList(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")

	out, err := h.agentListRun(context.Background(), args())
	if err != nil || !strings.Contains(out, "No agents") {
		t.Fatalf("empty list: out=%q err=%v", out, err)
	}

	if _, err := h.agentCreateRun(context.Background(), args("name", "Roblox Dev", "soul", "PERSONA", "mcp", "neublox serve --project {{WORKTREE}}", "backend", "codex", "cmd", "codex --model gpt-5.6")); err != nil {
		t.Fatal(err)
	}
	if a, ok := h.agents.Get("roblox-dev"); !ok || a.Backend != "codex" || a.Cmd != "codex --model gpt-5.6" {
		t.Fatalf("agent should exist under slug roblox-dev")
	}

	out, err = h.agentListRun(context.Background(), args())
	if err != nil || !strings.Contains(out, "roblox-dev") {
		t.Fatalf("list should mention agent: out=%q err=%v", out, err)
	}
}

func TestAgentCreateRejectsBadName(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")
	if _, err := h.agentCreateRun(context.Background(), args("name", "🙂")); err == nil {
		t.Fatal("expected rejection of unusable name")
	}
}

func TestAgentCreateMissingName(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")
	if _, err := h.agentCreateRun(context.Background(), args()); err == nil {
		t.Fatal("expected error when name missing")
	}
}

func TestSessionCreateWithAgentMaterializes(t *testing.T) {
	h, _, _, wt, _, st := newTestHandler(t, "")
	worktreeDir := t.TempDir()
	wt.path = worktreeDir
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})
	if _, err := h.agents.Create(agent.CreateSpec{Name: "roblox", Soul: "PERSONA", MCP: "neublox serve --project {{WORKTREE}}", Backend: "codex"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "agent", "roblox")); err != nil {
		t.Fatal(err)
	}

	mcp, err := os.ReadFile(filepath.Join(worktreeDir, ".mcp.json"))
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}
	if !strings.Contains(string(mcp), worktreeDir) || strings.Contains(string(mcp), "{{WORKTREE}}") {
		t.Fatalf(".mcp.json not substituted:\n%s", mcp)
	}
	if !strings.Contains(string(mcp), `"neublox"`) {
		t.Fatalf(".mcp.json missing neublox:\n%s", mcp)
	}

	settings, err := os.ReadFile(filepath.Join(worktreeDir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	if !strings.Contains(string(settings), "enableAllProjectMcpServers") || !strings.Contains(string(settings), "mcp__neublox__*") {
		t.Fatalf("settings.json wrong:\n%s", settings)
	}

	claude, err := os.ReadFile(filepath.Join(worktreeDir, ".claude", "CLAUDE.md"))
	if err != nil || string(claude) != "PERSONA" {
		t.Fatalf("CLAUDE.md = %q err=%v", claude, err)
	}

	sess, _ := st.FindSession("demo")
	if sess.Agent != "roblox" {
		t.Fatalf("session.Agent = %q, want roblox", sess.Agent)
	}
	if sess.Vendor != "codex" {
		t.Fatalf("session.Vendor = %q, want codex inherited from agent", sess.Vendor)
	}
}

func TestSessionCreateInheritsAgentCmd(t *testing.T) {
	h, _, _, wt, _, st := newTestHandler(t, "default-cmd")
	wt.path = t.TempDir()
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})
	if _, err := h.agents.Create(agent.CreateSpec{Name: "bob", Soul: "P", Backend: "codex", Cmd: "codex --model gpt-5.6"}); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "s1", "agent", "bob")); err != nil {
		t.Fatalf("create s1: %v", err)
	}
	s1, _ := st.FindSession("s1")
	if s1.Cmd != "codex --model gpt-5.6" {
		t.Fatalf("s1.Cmd = %q, want inherited agent cmd", s1.Cmd)
	}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "s2", "agent", "bob", "cmd", "claude")); err != nil {
		t.Fatalf("create s2: %v", err)
	}
	s2, _ := st.FindSession("s2")
	if s2.Cmd != "claude" {
		t.Fatalf("s2.Cmd = %q, want explicit override", s2.Cmd)
	}
}

func TestSessionCreateUnknownAgentRollsBack(t *testing.T) {
	h, d, _, wt, _, st := newTestHandler(t, "")
	worktreeDir := t.TempDir()
	wt.path = worktreeDir
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "agent", "ghost")); err == nil {
		t.Fatal("expected error for unknown agent")
	}
	if len(wt.removed) != 1 {
		t.Fatalf("worktree should be rolled back: %+v", wt.removed)
	}
	if len(d.created) != 0 {
		t.Fatalf("no channel should be created: %+v", d.created)
	}
	if _, ok := st.FindSession("demo"); ok {
		t.Fatal("session must not persist")
	}
}

func TestSessionCreateAgentRequiresWorktree(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})
	if _, err := h.agents.Create(agent.CreateSpec{Name: "roblox"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "agent", "roblox", "shared", "true")); err == nil {
		t.Fatal("expected error: agent session needs an isolated worktree")
	}
}

func TestSessionCreateNoAgentUnchanged(t *testing.T) {
	h, _, _, wt, _, st := newTestHandler(t, "")
	worktreeDir := t.TempDir()
	wt.path = worktreeDir
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(worktreeDir, ".mcp.json")); err == nil {
		t.Fatal("no agent → no provisioning files should be written")
	}
	sess, _ := st.FindSession("demo")
	if sess.Agent != "" {
		t.Fatalf("session.Agent should be empty, got %q", sess.Agent)
	}
}

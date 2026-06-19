package manager

import (
	"context"
	"strings"
	"testing"
)

func TestAgentCreateAndList(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")

	out, err := h.agentListRun(context.Background(), args())
	if err != nil || !strings.Contains(out, "No agents") {
		t.Fatalf("empty list: out=%q err=%v", out, err)
	}

	if _, err := h.agentCreateRun(context.Background(), args("name", "Roblox Dev", "soul", "PERSONA", "mcp", "neublox serve --project {{WORKTREE}}")); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.agents.Get("roblox-dev"); !ok {
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

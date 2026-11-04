package manager

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSessionCreateTakesTheConfiguredDefaultModel(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	h.SetDefaultModel("gw-claude-opus-5")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatal(err)
	}
	sess, ok := st.FindSession("demo")
	if !ok {
		t.Fatal("session not persisted")
	}
	if sess.ModelID != "gw-claude-opus-5" {
		t.Fatalf("ModelID = %q, want the configured default", sess.ModelID)
	}
}

func TestAnExplicitModelBeatsTheDefault(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude", "gw-claude-sonnet-5": "claude"})
	h.SetDefaultModel("gw-claude-opus-5")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "model", "gw-claude-sonnet-5")); err != nil {
		t.Fatal(err)
	}
	if sess, _ := st.FindSession("demo"); sess.ModelID != "gw-claude-sonnet-5" {
		t.Fatalf("ModelID = %q, want the explicitly requested model", sess.ModelID)
	}
}

func TestAnExplicitCmdDoesNotTakeTheDefaultModel(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	h.SetDefaultModel("gw-claude-opus-5")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "cmd", "claude --model whatever")); err != nil {
		t.Fatal(err)
	}
	if sess, _ := st.FindSession("demo"); sess.ModelID != "" {
		t.Fatalf("ModelID = %q, want none: the cmd names its own model", sess.ModelID)
	}
}

func TestTheDefaultIsSkippedWhenAnotherVendorIsRequested(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	h.SetDefaultModel("gw-claude-opus-5")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "vendor", "codex")); err != nil {
		t.Fatalf("a codex session was rejected because of a claude default: %v", err)
	}
	if sess, _ := st.FindSession("demo"); sess.ModelID != "" {
		t.Fatalf("ModelID = %q, want none: the default belongs to another vendor", sess.ModelID)
	}
}

func TestTheDefaultSatisfiesTheGatewayOnlyModelRequirement(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	gatewayOnly(h)
	h.SetDefaultModel("gw-claude-opus-5")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatalf("a modelless create was refused despite a configured default: %v", err)
	}
	if sess, _ := st.FindSession("demo"); sess.ModelID != "gw-claude-opus-5" {
		t.Fatalf("ModelID = %q", sess.ModelID)
	}
}

func TestNoDefaultLeavesTheModelEmpty(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatal(err)
	}
	if sess, _ := st.FindSession("demo"); sess.ModelID != "" {
		t.Fatalf("ModelID = %q, want none", sess.ModelID)
	}
}

func TestAnAgentModelBeatsTheVendorAndGlobalDefaults(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude", "gw-claude-sonnet-5": "claude", "gw-claude-haiku-5": "claude"})
	h.SetDefaultModel("gw-claude-opus-5")
	h.SetVendorDefaultModel(func(vendor string) string {
		if vendor == "claude" {
			return "gw-claude-sonnet-5"
		}
		return ""
	})
	profile, err := h.agents.Create(agent.CreateSpec{Name: "bob", Backend: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if writeErr := os.WriteFile(filepath.Join(profile.Home, "model"), []byte("gw-claude-haiku-5\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	runtime, err := h.chooseRuntime(args("name", "demo", "agent", "bob"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.model != "gw-claude-haiku-5" {
		t.Fatalf("model = %q, want the agent's model", runtime.model)
	}
}

func TestTheVendorDefaultBeatsTheGlobalDefault(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude", "gw-claude-sonnet-5": "claude"})
	h.SetDefaultModel("gw-claude-opus-5")
	h.SetVendorDefaultModel(func(vendor string) string {
		if vendor == "claude" {
			return "gw-claude-sonnet-5"
		}
		return ""
	})
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "vendor", "claude")); err != nil {
		t.Fatal(err)
	}
	if sess, _ := st.FindSession("demo"); sess.ModelID != "gw-claude-sonnet-5" {
		t.Fatalf("ModelID = %q, want the vendor default", sess.ModelID)
	}
}

func TestTheVendorDefaultIsSkippedForAnotherVendor(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-sonnet-5": "claude"})
	h.SetVendorDefaultModel(func(vendor string) string {
		if vendor == "claude" {
			return "gw-claude-sonnet-5"
		}
		return ""
	})
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "vendor", "codex")); err != nil {
		t.Fatalf("a codex session was rejected because of a claude vendor default: %v", err)
	}
	if sess, _ := st.FindSession("demo"); sess.ModelID != "" {
		t.Fatalf("ModelID = %q, want none", sess.ModelID)
	}
}

func TestAnExplicitModelBeatsTheAgentModel(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude", "gw-claude-haiku-5": "claude"})
	profile, err := h.agents.Create(agent.CreateSpec{Name: "bob", Backend: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if writeErr := os.WriteFile(filepath.Join(profile.Home, "model"), []byte("gw-claude-haiku-5\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	runtime, err := h.chooseRuntime(args("name", "demo", "agent", "bob", "model", "gw-claude-opus-5"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.model != "gw-claude-opus-5" {
		t.Fatalf("model = %q, want the explicit model", runtime.model)
	}
}

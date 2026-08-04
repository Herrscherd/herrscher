package manager

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSessionCreateTakesTheConfiguredDefaultModel(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	h.SetDefaultModel("gw-claude-opus-5")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

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

// A default is a fallback, never an override.
func TestAnExplicitModelBeatsTheDefault(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude", "gw-claude-sonnet-5": "claude"})
	h.SetDefaultModel("gw-claude-opus-5")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "model", "gw-claude-sonnet-5")); err != nil {
		t.Fatal(err)
	}
	if sess, _ := st.FindSession("demo"); sess.ModelID != "gw-claude-sonnet-5" {
		t.Fatalf("ModelID = %q, want the explicitly requested model", sess.ModelID)
	}
}

// An explicit cmd carries its own invocation. Attaching a model to it would give
// the spawn a gateway environment for a model the argv does not name, so the
// session would report one model and run another.
func TestAnExplicitCmdDoesNotTakeTheDefaultModel(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	h.SetDefaultModel("gw-claude-opus-5")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "cmd", "claude --model whatever")); err != nil {
		t.Fatal(err)
	}
	if sess, _ := st.FindSession("demo"); sess.ModelID != "" {
		t.Fatalf("ModelID = %q, want none: the cmd names its own model", sess.ModelID)
	}
}

// An explicit --vendor outranks a default that belongs to another backend:
// otherwise a correctly written command would fail validation on a preference
// the operator never restated.
func TestTheDefaultIsSkippedWhenAnotherVendorIsRequested(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	h.SetDefaultModel("gw-claude-opus-5")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "vendor", "codex")); err != nil {
		t.Fatalf("a codex session was rejected because of a claude default: %v", err)
	}
	if sess, _ := st.FindSession("demo"); sess.ModelID != "" {
		t.Fatalf("ModelID = %q, want none: the default belongs to another vendor", sess.ModelID)
	}
}

// The default is what makes gateway-only usable: with one configured, a plain
// `session create` is accepted even though a model is mandatory there.
func TestTheDefaultSatisfiesTheGatewayOnlyModelRequirement(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	gatewayOnly(h)
	h.SetDefaultModel("gw-claude-opus-5")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatalf("a modelless create was refused despite a configured default: %v", err)
	}
	if sess, _ := st.FindSession("demo"); sess.ModelID != "gw-claude-opus-5" {
		t.Fatalf("ModelID = %q", sess.ModelID)
	}
}

// No default configured = the behaviour that predates this feature.
func TestNoDefaultLeavesTheModelEmpty(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatal(err)
	}
	if sess, _ := st.FindSession("demo"); sess.ModelID != "" {
		t.Fatalf("ModelID = %q, want none", sess.ModelID)
	}
}

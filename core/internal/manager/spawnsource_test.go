package manager

import (
	"context"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

// gatewayOnly puts the handler under the public build's route policy.
func gatewayOnly(h *Handler) { h.SetGatewayOnly(func() bool { return true }) }

// A free-form cmd is argv the catalog never sees: it can name any binary and
// any --model, so it bills our account for a model nobody selected, and it
// skips the route policy entirely.
func TestSessionCreateRefusesExplicitCmdUnderGatewayOnly(t *testing.T) {
	h, _, sup, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	gatewayOnly(h)
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	_, err := h.sessionCreateRun(context.Background(), args(
		"name", "demo", "model", "gw-claude-opus-5", "cmd", "claude --model opus-expensive"))
	if err == nil {
		t.Fatal("create accepted a free-form cmd under gateway-only")
	}
	if !strings.Contains(err.Error(), "cmd") {
		t.Fatalf("error does not name the refused input: %v", err)
	}
	if _, ok := st.FindSession("demo"); ok {
		t.Fatal("session persisted despite the refused cmd")
	}
	if len(sup.started) != 0 {
		t.Fatalf("bridge started despite the refused cmd: %v", sup.started)
	}
}

// The other half of the hole: no model at all skips the catalog lookup, so the
// spawn gets no gateway environment and runs on the machine's own login.
func TestSessionCreateRequiresAModelUnderGatewayOnly(t *testing.T) {
	h, _, sup, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	gatewayOnly(h)
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	_, err := h.sessionCreateRun(context.Background(), args("name", "demo"))
	if err == nil {
		t.Fatal("create accepted a modelless session under gateway-only")
	}
	if !strings.Contains(err.Error(), "model") {
		t.Fatalf("error does not name the missing input: %v", err)
	}
	if _, ok := st.FindSession("demo"); ok {
		t.Fatal("session persisted despite having no model")
	}
	if len(sup.started) != 0 {
		t.Fatalf("bridge started despite having no model: %v", sup.started)
	}
}

// The supported public-build shape still works: pick a catalog model, let the
// host derive the invocation.
func TestSessionCreateWithModelAndNoCmdIsAcceptedUnderGatewayOnly(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	gatewayOnly(h)
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "model", "gw-claude-opus-5")); err != nil {
		t.Fatalf("the supported gateway-only shape was refused: %v", err)
	}
	sess, ok := st.FindSession("demo")
	if !ok {
		t.Fatal("session not persisted")
	}
	if sess.ModelID != "gw-claude-opus-5" {
		t.Fatalf("persisted ModelID = %q", sess.ModelID)
	}
}

// Non-regression for the INTERNAL build, which is what the colleagues run
// today: with no policy set, a free-form cmd and a modelless session are both
// legitimate — the machine's own login is the intended account there.
func TestExplicitCmdAndNoModelStayAcceptedWithoutTheGatewayPolicy(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args("name", "a", "cmd", "claude --model whatever")); err != nil {
		t.Fatalf("explicit cmd refused on the internal build: %v", err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "b")); err != nil {
		t.Fatalf("modelless create refused on the internal build: %v", err)
	}
}

// Switching a live session is the same escape by another door.
func TestSessionSwitchRefusesExplicitCmdUnderGatewayOnly(t *testing.T) {
	h, _, sup, _, _, st := newTestHandler(t, "category")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	gatewayOnly(h)
	if err := st.AddSession(state.Session{
		Name: "alpha", Vendor: "claude", Cmd: "claude", ModelID: "gw-claude-opus-5",
	}); err != nil {
		t.Fatal(err)
	}
	h.SetSeeder(func(string, string) bool { return true })

	_, err := h.sessionSwitchRun(context.Background(), args(
		"name", "alpha", "cmd", "claude --model opus-expensive", "handoff", "none"))
	if err == nil {
		t.Fatal("switch accepted a free-form cmd under gateway-only")
	}
	if got, _ := st.FindSession("alpha"); got.Cmd != "claude" {
		t.Fatalf("state mutated on a refused switch: %+v", got)
	}
	if len(sup.restarted) != 0 {
		t.Fatalf("restarted on a refused switch: %+v", sup.restarted)
	}
}

// A switch that supplies no --cmd keeps the session's current one, which the
// policy already vetted at creation: it must not be refused.
func TestSessionSwitchWithoutCmdStaysAcceptedUnderGatewayOnly(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude", "gw-claude-sonnet-5": "claude"})
	gatewayOnly(h)
	if err := st.AddSession(state.Session{
		Name: "alpha", Vendor: "claude", Cmd: "claude", ModelID: "gw-claude-opus-5",
	}); err != nil {
		t.Fatal(err)
	}
	h.SetSeeder(func(string, string) bool { return true })

	if _, err := h.sessionSwitchRun(context.Background(), args(
		"name", "alpha", "model", "gw-claude-sonnet-5", "handoff", "none")); err != nil {
		t.Fatalf("a model-only switch was refused under gateway-only: %v", err)
	}
	if got, _ := st.FindSession("alpha"); got.ModelID != "gw-claude-sonnet-5" {
		t.Fatalf("switch not applied: %+v", got)
	}
}

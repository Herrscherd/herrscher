package manager

import (
	"context"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func gatewayOnly(h *Handler) { h.SetGatewayOnly(func() bool { return true }) }

func TestSessionCreateRefusesExplicitCmdUnderGatewayOnly(t *testing.T) {
	h, _, sup, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	gatewayOnly(h)
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}

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

func TestSessionCreateRequiresAModelUnderGatewayOnly(t *testing.T) {
	h, _, sup, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	gatewayOnly(h)
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}

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

func TestSessionCreateWithModelAndNoCmdIsAcceptedUnderGatewayOnly(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	gatewayOnly(h)
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}

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

func TestExplicitCmdAndNoModelStayAcceptedWithoutTheGatewayPolicy(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "a", "cmd", "claude --model whatever")); err != nil {
		t.Fatalf("explicit cmd refused on the internal build: %v", err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "b")); err != nil {
		t.Fatalf("modelless create refused on the internal build: %v", err)
	}
}

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

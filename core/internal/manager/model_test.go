package manager

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

// knownModels wires a validator accepting exactly the given ids, mirroring what
// the composition root builds from host.LookupModel + the route policy.
func knownModels(h *Handler, ids ...string) {
	h.SetModelValidator(func(modelID string) error {
		for _, id := range ids {
			if id == modelID {
				return nil
			}
		}
		return errors.New("unknown model " + strconv.Quote(modelID))
	})
}

// TestSessionCreatePersistsModelID pins the `ModelID: modelID` field on the
// state.Session built for category/terminal homes. Drop it and the session is
// persisted with an empty model: the supervisor then spawns the bridge with no
// --model, the routing choke point is skipped, and a gateway model runs on the
// machine's own vendor login while the UI shows it selected.
func TestSessionCreatePersistsModelID(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	knownModels(h, "gw-claude-opus-5")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "model", "gw-claude-opus-5")); err != nil {
		t.Fatalf("create: %v", err)
	}
	sess, ok := st.FindSession("demo")
	if !ok {
		t.Fatal("session not persisted")
	}
	if sess.ModelID != "gw-claude-opus-5" {
		t.Fatalf("persisted ModelID = %q, want the created model", sess.ModelID)
	}
}

// Same line, the forum branch: it is a separate state.Session literal, so it
// can lose ModelID independently of the category one.
func TestSessionCreateForumPersistsModelID(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "forum")
	knownModels(h, "gw-claude-opus-5")
	st.SetHome(state.HomeRef{ID: "forum1", Type: "forum"})

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "model", "gw-claude-opus-5")); err != nil {
		t.Fatalf("create: %v", err)
	}
	sess, ok := st.FindSession("demo")
	if !ok {
		t.Fatal("session not persisted")
	}
	if sess.Type != "forum" {
		t.Fatalf("expected the forum construction site, got type %q", sess.Type)
	}
	if sess.ModelID != "gw-claude-opus-5" {
		t.Fatalf("persisted ModelID = %q, want the created model", sess.ModelID)
	}
}

// An unknown (or route-policy-excluded) id must be refused at creation, naming
// it, rather than persisted and surfaced much later as an opaque spawn failure.
func TestSessionCreateRejectsUnknownModel(t *testing.T) {
	h, _, sup, _, _, st := newTestHandler(t, "")
	knownModels(h, "gw-claude-opus-5")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	_, err := h.sessionCreateRun(context.Background(), args("name", "demo", "model", "gw-claude-opus-4"))
	if err == nil {
		t.Fatal("create accepted an unknown model id")
	}
	if !strings.Contains(err.Error(), "gw-claude-opus-4") {
		t.Fatalf("error does not name the model: %v", err)
	}
	if _, ok := st.FindSession("demo"); ok {
		t.Fatal("session persisted despite the unknown model")
	}
	if len(sup.started) != 0 {
		t.Fatalf("bridge started despite the unknown model: %v", sup.started)
	}
}

// A session with no model at all is the legacy path and stays accepted.
func TestSessionCreateWithoutModelIsAccepted(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	knownModels(h, "gw-claude-opus-5")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatalf("legacy create (no model) refused: %v", err)
	}
	if sess, _ := st.FindSession("demo"); sess.ModelID != "" {
		t.Fatalf("ModelID = %q, want empty", sess.ModelID)
	}
}

// An ABSENT --model keeps the session's current model. Re-targeting only the
// cmd of a gateway session must not silently drop it back to the legacy path,
// which would move it off the routed spawn — away from the safe direction.
func TestSessionSwitchWithoutModelKeepsCurrentModel(t *testing.T) {
	h, _, sup, _, _, st := newTestHandler(t, "category")
	knownModels(h, "gw-claude-opus-5")
	if err := st.AddSession(state.Session{
		Name: "alpha", Vendor: "claude", Cmd: "claude", ModelID: "gw-claude-opus-5",
	}); err != nil {
		t.Fatal(err)
	}
	h.SetSeeder(func(string, string) bool { return true })

	if _, err := h.sessionSwitchRun(context.Background(), args(
		"name", "alpha", "vendor", "claude", "cmd", "claude --effort high", "handoff", "none",
	)); err != nil {
		t.Fatalf("switch: %v", err)
	}
	got, _ := st.FindSession("alpha")
	if got.ModelID != "gw-claude-opus-5" {
		t.Fatalf("ModelID = %q after a switch with no --model, want it kept", got.ModelID)
	}
	if len(sup.restarted) != 1 || sup.restarted[0].ModelID != "gw-claude-opus-5" {
		t.Fatalf("restart must carry the kept model: %+v", sup.restarted)
	}
}

// An explicitly supplied empty --model clears it, back to the legacy path.
func TestSessionSwitchEmptyModelClearsIt(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	knownModels(h, "gw-claude-opus-5")
	if err := st.AddSession(state.Session{
		Name: "alpha", Vendor: "claude", Cmd: "claude", ModelID: "gw-claude-opus-5",
	}); err != nil {
		t.Fatal(err)
	}
	h.SetSeeder(func(string, string) bool { return true })

	if _, err := h.sessionSwitchRun(context.Background(), args(
		"name", "alpha", "vendor", "codex", "cmd", "codex", "model", "", "handoff", "none",
	)); err != nil {
		t.Fatalf("switch: %v", err)
	}
	if got, _ := st.FindSession("alpha"); got.ModelID != "" {
		t.Fatalf("ModelID = %q, want cleared by an explicit empty --model", got.ModelID)
	}
}

// Switching TO an unknown model is refused before anything is persisted or
// restarted.
func TestSessionSwitchRejectsUnknownModel(t *testing.T) {
	h, _, sup, _, _, st := newTestHandler(t, "category")
	knownModels(h, "gw-claude-opus-5")
	if err := st.AddSession(state.Session{
		Name: "alpha", Vendor: "claude", Cmd: "claude", ModelID: "gw-claude-opus-5",
	}); err != nil {
		t.Fatal(err)
	}

	_, err := h.sessionSwitchRun(context.Background(), args(
		"name", "alpha", "vendor", "claude", "cmd", "claude", "model", "nope", "handoff", "none",
	))
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("switch error = %v, want one naming the unknown model", err)
	}
	if got, _ := st.FindSession("alpha"); got.ModelID != "gw-claude-opus-5" {
		t.Fatalf("state mutated on a refused switch: %+v", got)
	}
	if len(sup.restarted) != 0 {
		t.Fatalf("restarted on a refused switch: %+v", sup.restarted)
	}
}

// TestSessionSwitchRollbackRestoresModelID pins the oldModelID argument to
// rollbackSwitch. Neutralised to "", a failed switch leaves the session with a
// CLEARED model instead of the prior one: it comes back up on the legacy path,
// off the gateway, silently.
func TestSessionSwitchRollbackRestoresModelID(t *testing.T) {
	h, _, sup, _, _, st := newTestHandler(t, "category")
	knownModels(h, "gw-claude-opus-5", "native-gpt")
	if err := st.AddSession(state.Session{
		Name: "gamma", Vendor: "claude", Cmd: "claude", ModelID: "gw-claude-opus-5", ResumeToken: "tok-old",
	}); err != nil {
		t.Fatal(err)
	}
	h.SetSeeder(func(string, string) bool { return true })
	// The new backend's Start fails; the rollback Start succeeds.
	sup.startErrs = []error{errors.New("boom")}

	if _, err := h.sessionSwitchRun(context.Background(), args(
		"name", "gamma", "vendor", "codex", "cmd", "codex", "model", "native-gpt", "handoff", "none",
	)); err == nil {
		t.Fatal("expected an error when the restart fails")
	}
	got, _ := st.FindSession("gamma")
	if got.ModelID != "gw-claude-opus-5" {
		t.Fatalf("ModelID = %q after rollback, want the prior model restored", got.ModelID)
	}
	if len(sup.restarted) != 2 || sup.restarted[1].ModelID != "gw-claude-opus-5" {
		t.Fatalf("rollback restart must carry the prior model: %+v", sup.restarted)
	}
}

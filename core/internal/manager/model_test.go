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
	h.SetModelValidator(func(_ string, modelID string) error {
		for _, id := range ids {
			if id == modelID {
				return nil
			}
		}
		return errors.New("unknown model " + strconv.Quote(modelID))
	})
}

// ownedModels wires a vendor-aware validator, mirroring what the composition
// root builds from host.LookupModel: the catalog knows which backend declares
// each model, so a vendor that does not own it is refused.
func ownedModels(h *Handler, owner map[string]string) {
	h.SetModelValidator(func(vendor, modelID string) error {
		own, ok := owner[modelID]
		if !ok {
			return errors.New("unknown model " + strconv.Quote(modelID))
		}
		if vendor != "" && vendor != own {
			return errors.New("model " + strconv.Quote(modelID) + " belongs to backend " +
				strconv.Quote(own) + ", not " + strconv.Quote(vendor))
		}
		return nil
	})
}

// TestSessionCreateRejectsVendorModelMismatch: --vendor wins when the backend
// is selected, but the spawn environment is keyed off the model's OWNING
// vendor. A mismatch spawns codex with ANTHROPIC_* it ignores — the turn runs
// on the machine's own ChatGPT login while the session reads gateway-routed.
func TestSessionCreateRejectsVendorModelMismatch(t *testing.T) {
	h, _, sup, _, _, st := newTestHandler(t, "")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	_, err := h.sessionCreateRun(context.Background(), args(
		"name", "demo", "vendor", "codex", "model", "gw-claude-opus-5"))
	if err == nil {
		t.Fatal("create accepted a codex session carrying a claude model")
	}
	if !strings.Contains(err.Error(), "codex") || !strings.Contains(err.Error(), "gw-claude-opus-5") {
		t.Fatalf("error names neither the vendor nor the model: %v", err)
	}
	if _, ok := st.FindSession("demo"); ok {
		t.Fatal("session persisted despite the vendor/model mismatch")
	}
	if len(sup.started) != 0 {
		t.Fatalf("bridge started despite the mismatch: %v", sup.started)
	}
}

// The reported path: `session switch --vendor codex` on a gateway session with
// no --model. The model is RETAINED, so the pair only becomes inconsistent
// because the vendor moved — validating the supplied model alone misses it.
func TestSessionSwitchRejectsVendorMismatchOnRetainedModel(t *testing.T) {
	h, _, sup, _, _, st := newTestHandler(t, "category")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude"})
	if err := st.AddSession(state.Session{
		Name: "alpha", Vendor: "claude", Cmd: "claude", ModelID: "gw-claude-opus-5",
	}); err != nil {
		t.Fatal(err)
	}
	h.SetSeeder(func(string, string) bool { return true })

	_, err := h.sessionSwitchRun(context.Background(), args(
		"name", "alpha", "vendor", "codex", "cmd", "codex", "handoff", "none"))
	if err == nil {
		t.Fatal("switch to codex kept a claude model: the session would silently run on the local login")
	}
	if !strings.Contains(err.Error(), "codex") || !strings.Contains(err.Error(), "gw-claude-opus-5") {
		t.Fatalf("error names neither the vendor nor the model: %v", err)
	}
	if got, _ := st.FindSession("alpha"); got.Vendor != "claude" || got.ModelID != "gw-claude-opus-5" {
		t.Fatalf("state mutated on a refused switch: %+v", got)
	}
	if len(sup.restarted) != 0 {
		t.Fatalf("restarted on a refused switch: %+v", sup.restarted)
	}
}

// Non-regression: switching vendor AND model together, consistently, still works.
func TestSessionSwitchAcceptsMatchingVendorAndModel(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	ownedModels(h, map[string]string{"gw-claude-opus-5": "claude", "gpt-5.5": "codex"})
	if err := st.AddSession(state.Session{
		Name: "alpha", Vendor: "claude", Cmd: "claude", ModelID: "gw-claude-opus-5",
	}); err != nil {
		t.Fatal(err)
	}
	h.SetSeeder(func(string, string) bool { return true })

	if _, err := h.sessionSwitchRun(context.Background(), args(
		"name", "alpha", "vendor", "codex", "cmd", "codex", "model", "gpt-5.5", "handoff", "none",
	)); err != nil {
		t.Fatalf("consistent vendor+model switch refused: %v", err)
	}
	if got, _ := st.FindSession("alpha"); got.Vendor != "codex" || got.ModelID != "gpt-5.5" {
		t.Fatalf("switch not applied: %+v", got)
	}
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

// Third state.Session literal, added by the channel-adoption path: a session
// bound to an existing conversation. Losing ModelID here is silent and
// dangerous — the session spawns with no gateway environment, i.e. on the
// machine's own vendor login, while the operator believes they picked a
// gateway model.
func TestSessionCreateAdoptedChannelPersistsModelID(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	knownModels(h, "gw-claude-opus-5")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "channel_id", "123", "model", "gw-claude-opus-5")); err != nil {
		t.Fatalf("create with an adopted channel: %v", err)
	}
	sess, ok := st.FindSession("demo")
	if !ok {
		t.Fatal("session not persisted")
	}
	if sess.ChannelID != "123" {
		t.Fatalf("session not bound to the adopted channel: %+v", sess)
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
	// Stopping for the new backend fails; the rollback restart succeeds.
	sup.stopErrs = []error{errors.New("boom")}

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

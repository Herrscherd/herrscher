package manager

import (
	"context"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func seedModeSession(t *testing.T, st *state.State, name, vendor, approvals string) {
	t.Helper()
	if err := st.AddSession(state.Session{Name: name, Vendor: vendor, Approvals: approvals}); err != nil {
		t.Fatal(err)
	}
}

func setModeOf(t *testing.T, h *Handler, name, mode string) (string, error) {
	t.Helper()
	return h.sessionSetModeRun(context.Background(), args("name", name, "mode", mode))
}

func storedMode(t *testing.T, st *state.State, name string) string {
	t.Helper()
	sess, ok := st.FindSession(name)
	if !ok {
		t.Fatalf("session %q vanished", name)
	}
	return sess.Approvals
}

func TestSetModeStoresTheModeTheHostEnforces(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	h.SetGateResolver(func(string) (string, string) { return "tool", "" })
	seedModeSession(t, st, "s1", "claude", "ask")

	for _, mode := range []string{"accept-edits", "plan", "ask", "bypass"} {
		if _, err := setModeOf(t, h, "s1", mode); err != nil {
			t.Fatalf("set %s: %v", mode, err)
		}
		if got := storedMode(t, st, "s1"); got != mode {
			t.Fatalf("stored %q, want %q", got, mode)
		}
	}
}

func TestSetModeRefusesPlanOnABackendThatCannotEnforceIt(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	h.SetGateResolver(func(string) (string, string) { return "turn", "the vendor exposes no tool hook" })
	seedModeSession(t, st, "s1", "cursor", "ask")

	_, err := setModeOf(t, h, "s1", "plan")
	if err == nil {
		t.Fatal("plan on an unenforced backend must be refused")
	}
	if got := storedMode(t, st, "s1"); got != "ask" {
		t.Fatalf("a refused switch must keep the mode, got %q", got)
	}
}

func TestSetModeWarnsWhenTheBackendCannotEnforceAnotherMode(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	h.SetGateResolver(func(string) (string, string) { return "turn", "the vendor exposes no tool hook" })
	seedModeSession(t, st, "s1", "cursor", "ask")

	reply, err := setModeOf(t, h, "s1", "accept-edits")
	if err != nil {
		t.Fatalf("accept-edits is allowed, it only warns: %v", err)
	}
	if !strings.Contains(reply, "runs ungated") {
		t.Fatalf("reply must say the session runs ungated, got %q", reply)
	}
}

func TestSetModeRejectsAnUnknownMode(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	seedModeSession(t, st, "s1", "claude", "ask")
	if _, err := setModeOf(t, h, "s1", "turbo"); err == nil {
		t.Fatal("an unknown mode must be refused")
	}
	if _, err := setModeOf(t, h, "s1", ""); err == nil {
		t.Fatal("an empty mode must be refused")
	}
}

func TestSetModeNeedsAKnownSession(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")
	if _, err := setModeOf(t, h, "ghost", "plan"); err == nil {
		t.Fatal("a missing session must be refused")
	}
}

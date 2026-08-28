package manager

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

// switch always re-targets the persisted backend; a non-"none" handoff also
// stops+restarts the supervised child and injects a context seed built from the
// prior transcript.
func TestSessionSwitchRetargetsAndRestartsWithSeed(t *testing.T) {
	h, _, sup, _, _, st := newTestHandler(t, "category")
	if err := st.AddSession(state.Session{Name: "alpha", Vendor: "claude", Cmd: "claude --model claude-opus-4-8", ResumeToken: "old"}); err != nil {
		t.Fatal(err)
	}
	// A prior transcript so the summary handoff has something to hand off.
	_ = state.AppendTranscript(state.TranscriptPath(h.PartDir(), "alpha"), state.TranscriptEntry{Role: "user", Text: "bonjour"})
	_ = state.AppendTranscript(state.TranscriptPath(h.PartDir(), "alpha"), state.TranscriptEntry{Role: "assistant", Text: "salut"})

	var seeded []string
	h.SetSeeder(func(name, task string) bool { seeded = append(seeded, name+"|"+task); return true })

	if _, err := h.sessionSwitchRun(context.Background(), args(
		"name", "alpha", "vendor", "codex", "cmd", "codex --model gpt-5-codex", "handoff", "summary",
	)); err != nil {
		t.Fatalf("switch: %v", err)
	}

	got, _ := st.FindSession("alpha")
	if got.Vendor != "codex" || got.ResumeToken != "" {
		t.Fatalf("state not re-targeted: %+v", got)
	}
	if len(sup.stopped) != 1 || len(sup.started) != 1 {
		t.Fatalf("expected one Stop then one Start, got stop=%v start=%v", sup.stopped, sup.started)
	}
	if len(seeded) != 1 {
		t.Fatalf("expected one seed injection, got %v", seeded)
	}
}

// handoff=none re-targets state AND restarts the backend so the new Cmd takes
// effect, but injects NO seed (the child adopts the new model with a fresh thread).
func TestSessionSwitchNoneRestartsWithoutSeed(t *testing.T) {
	h, _, sup, _, _, st := newTestHandler(t, "category")
	if err := st.AddSession(state.Session{Name: "beta", Vendor: "claude", Cmd: "claude"}); err != nil {
		t.Fatal(err)
	}
	var seeded []string
	h.SetSeeder(func(name, task string) bool { seeded = append(seeded, name); return true })

	if _, err := h.sessionSwitchRun(context.Background(), args(
		"name", "beta", "vendor", "codex", "cmd", "codex --model gpt-5-codex", "handoff", "none",
	)); err != nil {
		t.Fatalf("switch none: %v", err)
	}
	if len(sup.stopped) != 1 || len(sup.started) != 1 {
		t.Fatalf("handoff=none must restart the backend once, got stop=%v start=%v", sup.stopped, sup.started)
	}
	if len(sup.restarted) != 1 || sup.restarted[0].Name != "beta" ||
		sup.restarted[0].Vendor != "codex" ||
		sup.restarted[0].Cmd != "codex --model gpt-5-codex" {
		t.Fatalf("switch must atomically replace only beta with the persisted target, got %+v", sup.restarted)
	}
	if sup.startedCmds[0] != "codex --model gpt-5-codex" {
		t.Fatalf("restart must use the new Cmd, got %q", sup.startedCmds[0])
	}
	if len(seeded) != 0 {
		t.Fatalf("handoff=none must inject no seed, got %v", seeded)
	}
	if got, _ := st.FindSession("beta"); got.Vendor != "codex" {
		t.Fatal("state must still be re-targeted on none")
	}
}

// When the restart Start fails, the switch rolls back: state returns to the old
// vendor/cmd, the old resume token is restored, and the old backend is restarted.
func TestSessionSwitchRollsBackOnStartFailure(t *testing.T) {
	h, _, sup, _, _, st := newTestHandler(t, "category")
	if err := st.AddSession(state.Session{Name: "gamma", Vendor: "claude", Cmd: "claude --model claude-opus-4-8", ResumeToken: "tok-old"}); err != nil {
		t.Fatal(err)
	}
	h.SetSeeder(func(name, task string) bool { return true })
	// The first restart (to the new backend) fails; the rollback restart succeeds.
	sup.stopErrs = []error{errors.New("boom")}

	_, err := h.sessionSwitchRun(context.Background(), args(
		"name", "gamma", "vendor", "codex", "cmd", "codex --model gpt-5-codex", "handoff", "none",
	))
	if err == nil {
		t.Fatal("expected an error when the restart fails")
	}

	got, _ := st.FindSession("gamma")
	if got.Vendor != "claude" || got.Cmd != "claude --model claude-opus-4-8" {
		t.Fatalf("state not rolled back: %+v", got)
	}
	if got.ResumeToken != "tok-old" {
		t.Fatalf("resume token not restored: %q", got.ResumeToken)
	}
	// One Start call: the failed restart never got past Stop, so the only bridge
	// launched is the rollback's — and it must carry the old Cmd, or the session
	// is left running the backend the switch was rolled back from.
	if len(sup.started) != 1 {
		t.Fatalf("expected 1 Start call (the rollback only), got %v", sup.started)
	}
	if sup.startedCmds[0] != "claude --model claude-opus-4-8" {
		t.Fatalf("rollback restart must use the old Cmd, got %q", sup.startedCmds[0])
	}
}

// A vendor change can land a gated session on a backend that enforces nothing.
// The switch happens either way, and the loss of the guardrail is said out loud
// rather than left to be found out on the first tool call nobody was asked about.
func TestSessionSwitchWarnsWhenTheNewBackendCannotGate(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	if err := st.AddSession(state.Session{Name: "gamma", Vendor: "claude", Cmd: "claude", Approvals: "strict"}); err != nil {
		t.Fatal(err)
	}
	h.SetGateResolver(func(vendor string) (string, string) {
		if vendor == "claude" {
			return "tool", ""
		}
		return "", "cursor-agent exposes no permission hook"
	})
	out, err := h.sessionSwitchRun(context.Background(), args(
		"name", "gamma", "vendor", "cursor", "cmd", "cursor-agent", "handoff", "none",
	))
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if !strings.Contains(out, "runs ungated") || !strings.Contains(out, "no permission hook") {
		t.Fatalf("no warning in reply: %q", out)
	}
}

// The same switch onto a backend that does gate says nothing extra: a warning
// nobody needs is noise, and noise is what makes a real warning unreadable.
func TestSessionSwitchIsQuietWhenTheNewBackendGates(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	if err := st.AddSession(state.Session{Name: "delta", Vendor: "cursor", Cmd: "cursor-agent", Approvals: "strict"}); err != nil {
		t.Fatal(err)
	}
	h.SetGateResolver(func(string) (string, string) { return "tool", "" })
	out, err := h.sessionSwitchRun(context.Background(), args(
		"name", "delta", "vendor", "claude", "cmd", "claude", "handoff", "none",
	))
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if strings.Contains(out, "ungated") {
		t.Fatalf("unwanted warning: %q", out)
	}
}

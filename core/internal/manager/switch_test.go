package manager

import (
	"context"
	"errors"
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
	// First Start (the new backend) fails; the rollback Start (old backend) succeeds.
	sup.startErrs = []error{errors.New("boom")}

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
	// Two Start calls: the failed new backend, then the successful old-backend restart.
	if len(sup.started) != 2 {
		t.Fatalf("expected 2 Start calls (failed + rollback), got %v", sup.started)
	}
	if sup.startedCmds[1] != "claude --model claude-opus-4-8" {
		t.Fatalf("rollback restart must use the old Cmd, got %q", sup.startedCmds[1])
	}
}

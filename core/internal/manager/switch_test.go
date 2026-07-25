package manager

import (
	"context"
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

// handoff=none re-targets state only: no restart, no seed.
func TestSessionSwitchNoneSkipsRestart(t *testing.T) {
	h, _, sup, _, _, st := newTestHandler(t, "category")
	if err := st.AddSession(state.Session{Name: "beta", Vendor: "claude", Cmd: "claude"}); err != nil {
		t.Fatal(err)
	}
	h.SetSeeder(func(name, task string) bool { return true })

	if _, err := h.sessionSwitchRun(context.Background(), args(
		"name", "beta", "vendor", "codex", "cmd", "codex", "handoff", "none",
	)); err != nil {
		t.Fatalf("switch none: %v", err)
	}
	if len(sup.started) != 0 || len(sup.stopped) != 0 {
		t.Fatal("handoff=none must not restart the backend")
	}
	if got, _ := st.FindSession("beta"); got.Vendor != "codex" {
		t.Fatal("state must still be re-targeted on none")
	}
}

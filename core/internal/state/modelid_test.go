package state

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSessionModelIDRoundTrips(t *testing.T) {
	b, err := json.Marshal(Session{Name: "s", ModelID: "claude-opus-5"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"modelId":"claude-opus-5"`) {
		t.Fatalf("ModelID missing from JSON: %s", b)
	}
	var back Session
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.ModelID != "claude-opus-5" {
		t.Fatalf("ModelID = %q after round-trip", back.ModelID)
	}
}

func TestSessionModelIDOmittedWhenEmpty(t *testing.T) {
	// Sessions from before the catalog existed must not gain an empty field in
	// state.json — the file is re-read by older binaries.
	b, _ := json.Marshal(Session{Name: "s"})
	if strings.Contains(string(b), "modelId") {
		t.Fatalf("empty ModelID was serialized: %s", b)
	}
}

func TestSetBackendTargetWritesModelID(t *testing.T) {
	s := &State{Sessions: []Session{{Name: "s", Vendor: "claude", Cmd: "claude", ResumeToken: "old"}}}
	s.SetBackendTarget("s", "codex", "codex --model gpt-5.5", "gpt-5.5")
	got := s.Sessions[0]
	if got.ModelID != "gpt-5.5" {
		t.Errorf("ModelID = %q, want gpt-5.5", got.ModelID)
	}
	if got.Vendor != "codex" || got.Cmd != "codex --model gpt-5.5" {
		t.Errorf("vendor/cmd not rewritten: %+v", got)
	}
	if got.ResumeToken != "" {
		t.Errorf("resume token survived a backend switch: %q", got.ResumeToken)
	}
}

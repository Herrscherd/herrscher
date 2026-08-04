package state

import (
	"encoding/json"
	"path/filepath"
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

func TestSessionModelIDAbsentInLegacyStateFile(t *testing.T) {
	// A state.json written by a build that predates ModelID has no "modelId"
	// key at all. It must unmarshal cleanly with ModelID == "", not error and
	// not invent a value — this is what lets an old daemon's session survive
	// being read by a new binary.
	const legacy = `{"name":"s","vendor":"claude","cmd":"claude","resumeToken":"tok-1"}`
	var sess Session
	if err := json.Unmarshal([]byte(legacy), &sess); err != nil {
		t.Fatalf("unmarshal legacy state.json: %v", err)
	}
	if sess.ModelID != "" {
		t.Fatalf("ModelID = %q, want empty for a pre-catalog state.json", sess.ModelID)
	}
	if sess.Name != "s" || sess.Vendor != "claude" || sess.Cmd != "claude" || sess.ResumeToken != "tok-1" {
		t.Fatalf("other fields not preserved: %+v", sess)
	}
}

func TestSetBackendTargetWritesModelID(t *testing.T) {
	// A State with an empty path persists to "./.tmp" and then fails to rename
	// it into place, littering the package directory (the artifact was even
	// committed once). Give it a real path under t.TempDir() so the save lands.
	s, err := LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	s.Sessions = []Session{{Name: "s", Vendor: "claude", Cmd: "claude", ResumeToken: "old"}}
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

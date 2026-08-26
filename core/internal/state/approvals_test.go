package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestState(t *testing.T) *State {
	t.Helper()
	st, err := LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	return st
}

func TestApprovalRulesRoundTrip(t *testing.T) {
	st := newTestState(t)
	if len(st.ApprovalRules()) != 0 {
		t.Fatal("a fresh state must carry no rule")
	}
	if err := st.AddApprovalRule("ask Bash(git push*)"); err != nil {
		t.Fatalf("AddApprovalRule: %v", err)
	}
	if err := st.AddApprovalRule("ask Bash(git push*)"); err != nil {
		t.Fatalf("AddApprovalRule twice: %v", err)
	}
	if got := st.ApprovalRules(); len(got) != 1 {
		t.Fatalf("got %v, want one rule: adding the same rule twice must not double it", got)
	}
	removed, err := st.RemoveApprovalRule("ask Bash(git push*)")
	if err != nil || !removed {
		t.Fatalf("RemoveApprovalRule: removed=%v err=%v", removed, err)
	}
	if removed, _ := st.RemoveApprovalRule("ask Bash(git push*)"); removed {
		t.Fatal("removing an absent rule must report false")
	}
}

// No verb writes the wait: it is a field an operator edits in state.json, so
// what the file said is what every ask uses, and a value nothing can make sense
// of falls back to the default rather than taking the daemon down with it.
func TestApprovalWaitDefaultsAndParses(t *testing.T) {
	if got := newTestState(t).ApprovalWait(); got != 5*time.Minute {
		t.Fatalf("got %v, want the 5m default", got)
	}

	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"sessions":[],"approvalTimeout":"90s"}`), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	st, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got := st.ApprovalWait(); got != 90*time.Second {
		t.Fatalf("got %v, want the 90s the file asked for", got)
	}

	for _, stored := range []string{"whenever", "-1m", "0"} {
		st := newTestState(t)
		st.ApprovalTimeout = stored
		if got := st.ApprovalWait(); got != DefaultApprovalWait {
			t.Fatalf("approvalTimeout %q gave %v, want the default", stored, got)
		}
	}
}

func TestSetSessionApprovals(t *testing.T) {
	st := newTestState(t)
	if err := st.AddSession(Session{Name: "s1", ChannelID: "c1"}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if err := st.SetSessionApprovals("s1", "bypass"); err != nil {
		t.Fatalf("SetSessionApprovals: %v", err)
	}
	sessions := st.SnapshotSessions()
	if sessions[0].Approvals != "bypass" {
		t.Fatalf("got %q, want bypass", sessions[0].Approvals)
	}
	if err := st.SetSessionApprovals("nope", "bypass"); err == nil {
		t.Fatal("an unknown session must be an error, not a silent no-op")
	}
}

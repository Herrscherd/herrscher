package state

import (
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

func TestApprovalWaitDefaultsAndParses(t *testing.T) {
	st := newTestState(t)
	if got := st.ApprovalWait(); got != 5*time.Minute {
		t.Fatalf("got %v, want the 5m default", got)
	}
	if err := st.SetApprovalTimeout("90s"); err != nil {
		t.Fatalf("SetApprovalTimeout: %v", err)
	}
	if got := st.ApprovalWait(); got != 90*time.Second {
		t.Fatalf("got %v, want 90s", got)
	}
	if err := st.SetApprovalTimeout("bientôt"); err == nil {
		t.Fatal("an unparseable duration must be refused at the door")
	}
	if got := st.ApprovalWait(); got != 90*time.Second {
		t.Fatalf("got %v: a refused write must not have landed", got)
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

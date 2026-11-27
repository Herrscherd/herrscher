package state

import (
	"path/filepath"
	"testing"
)

func loadWithSession(t *testing.T) *State {
	t.Helper()
	st, err := LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(Session{Name: "alpha", PullRequestHeadSHA: "a"}); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestClaimCINudgeRefusesAStaleRecord(t *testing.T) {
	st := loadWithSession(t)
	first := CINudge{Number: 7, Attempts: 1, Head: "a"}
	if ok, err := st.ClaimCINudge("alpha", CINudge{}, first, "a"); !ok || err != nil {
		t.Fatalf("first claim = %v, %v, want applied", ok, err)
	}
	if ok, err := st.ClaimCINudge("alpha", CINudge{}, CINudge{Number: 7, Attempts: 2, Head: "b"}, "a"); ok || err != nil {
		t.Fatalf("stale claim = %v, %v, want refused", ok, err)
	}
	sess, _ := st.FindSession("alpha")
	if got := sess.CINudge(); got != first {
		t.Fatalf("record = %+v, want %+v", got, first)
	}
}

func TestClaimCINudgeRefusesAHeadThatIsNoLongerCurrent(t *testing.T) {
	st := loadWithSession(t)
	if ok, err := st.ClaimCINudge("alpha", CINudge{}, CINudge{Number: 7, Attempts: 1, Head: "old"}, "old"); ok || err != nil {
		t.Fatalf("claim on a head the session no longer has = %v, %v, want refused", ok, err)
	}
	sess, _ := st.FindSession("alpha")
	if got := sess.CINudge(); got != (CINudge{}) {
		t.Fatalf("record = %+v, want untouched", got)
	}
}

func TestReleaseCINudgePutsThePreviousRecordBack(t *testing.T) {
	st := loadWithSession(t)
	prev := CINudge{Number: 7, Attempts: 1, Head: "a"}
	reserved := CINudge{Number: 7, Attempts: 2, Head: "a"}
	if ok, err := st.ClaimCINudge("alpha", CINudge{}, prev, "a"); !ok || err != nil {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	if ok, err := st.ClaimCINudge("alpha", prev, reserved, "a"); !ok || err != nil {
		t.Fatalf("reserve = %v, %v", ok, err)
	}
	if err := st.ReleaseCINudge("alpha", reserved, prev); err != nil {
		t.Fatal(err)
	}
	sess, _ := st.FindSession("alpha")
	if got := sess.CINudge(); got != prev {
		t.Fatalf("record = %+v, want %+v", got, prev)
	}
}

package state

import (
	"path/filepath"
	"testing"
)

func TestCompareAndSetCINudgeRefusesAStaleRecord(t *testing.T) {
	st, err := LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(Session{Name: "alpha"}); err != nil {
		t.Fatal(err)
	}
	first := CINudge{Number: 7, Attempts: 1, Head: "a"}
	if ok, err := st.CompareAndSetCINudge("alpha", CINudge{}, first); !ok || err != nil {
		t.Fatalf("first swap = %v, %v, want applied", ok, err)
	}
	if ok, err := st.CompareAndSetCINudge("alpha", CINudge{}, CINudge{Number: 7, Attempts: 2, Head: "b"}); ok || err != nil {
		t.Fatalf("stale swap = %v, %v, want refused", ok, err)
	}
	sess, _ := st.FindSession("alpha")
	if got := sess.CINudge(); got != first {
		t.Fatalf("record = %+v, want %+v", got, first)
	}
}

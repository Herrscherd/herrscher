package manager

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestBudgetReason(t *testing.T) {
	cases := []struct {
		name                                       string
		sc                                         float64
		st                                         uint64
		cc, ctk                                    float64 // caps
		tkCap                                      uint64
		coc, coctk                                 float64 // cohort totals + caps unused fields below
		cohortCost                                 float64
		cohortTokens                               uint64
		cohortCostCap                              float64
		cohortTokenCap                             uint64
		want                                       string
	}{
		{name: "under all caps", sc: 1, st: 100, cc: 5, tkCap: 1000, want: ""},
		{name: "cost cap tripped", sc: 6, st: 100, cc: 5, tkCap: 1000, want: "cost"},
		{name: "token cap tripped", sc: 1, st: 2000, cc: 5, tkCap: 1000, want: "tokens"},
		{name: "uncapped never trips", sc: 999, st: 999999, want: ""},
		{name: "cohort cost tripped", sc: 1, st: 10, cohortCost: 12, cohortCostCap: 10, want: "cohort_cost"},
		{name: "cohort tokens tripped", sc: 1, st: 10, cohortTokens: 5000, cohortTokenCap: 4000, want: "cohort_tokens"},
		{name: "session wins over cohort", sc: 6, cc: 5, cohortCost: 12, cohortCostCap: 10, want: "cost"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := budgetReason(tc.sc, tc.st, tc.cc, tc.tkCap,
				tc.cohortCost, tc.cohortTokens, tc.cohortCostCap, tc.cohortTokenCap)
			if got != tc.want {
				t.Fatalf("budgetReason = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSessionBudgetGate_CheckAfterTurn proves the concrete gate re-derives usage
// from the transcript fold (single source of truth) and persists PausedReason
// when a cap trips, so a paused session comes back paused across reloads.
func TestSessionBudgetGate_CheckAfterTurn(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{{Name: "game", CostCap: 5}}
	if err := state.AppendTranscript(state.TranscriptPath(h.PartDir(), "game"), state.TranscriptEntry{
		Ts: "2026-07-27T00:00:00Z", Role: "assistant", Text: "done", Cost: 6,
	}); err != nil {
		t.Fatal(err)
	}

	gate := h.BudgetGate()
	reason := gate.CheckAfterTurn("game")
	if reason != "cost" {
		t.Fatalf("reason = %q, want %q", reason, "cost")
	}
	sess, ok := st.FindSession("game")
	if !ok || sess.PausedReason != "cost" {
		t.Fatalf("PausedReason not persisted: ok=%v sess=%+v", ok, sess)
	}
}

// TestSessionBudgetGate_UncappedNeverTrips proves a session with no caps
// configured never pauses and never gets a PausedReason written.
func TestSessionBudgetGate_UncappedNeverTrips(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{{Name: "free"}}
	if err := state.AppendTranscript(state.TranscriptPath(h.PartDir(), "free"), state.TranscriptEntry{
		Ts: "2026-07-27T00:00:00Z", Role: "assistant", Text: "done", Cost: 999,
	}); err != nil {
		t.Fatal(err)
	}

	gate := h.BudgetGate()
	reason := gate.CheckAfterTurn("free")
	if reason != "" {
		t.Fatalf("reason = %q, want empty", reason)
	}
	sess, ok := st.FindSession("free")
	if !ok || sess.PausedReason != "" {
		t.Fatalf("PausedReason must stay empty: ok=%v sess=%+v", ok, sess)
	}
}

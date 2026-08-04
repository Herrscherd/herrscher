package manager

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestBudgetReason(t *testing.T) {
	cases := []struct {
		name           string
		sc             float64
		st             uint64
		cc, ctk        float64 // caps
		tkCap          uint64
		coc, coctk     float64 // cohort totals + caps unused fields below
		cohortCost     float64
		cohortTokens   uint64
		cohortCostCap  float64
		cohortTokenCap uint64
		want           string
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

// TestSessionBudgetGate_CheckTrips proves the concrete gate re-derives usage
// from the transcript fold (single source of truth) and persists PausedReason
// when a cap trips, so a paused session comes back paused across reloads.
func TestSessionBudgetGate_CheckTrips(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{{Name: "game", CostCap: 5}}
	if err := state.AppendTranscript(state.TranscriptPath(h.PartDir(), "game"), state.TranscriptEntry{
		Ts: "2026-07-27T00:00:00Z", Role: "assistant", Text: "done", Cost: 6,
	}); err != nil {
		t.Fatal(err)
	}

	gate := h.BudgetGate()
	reason, _, _ := gate.Check("game")
	if reason != "cost" {
		t.Fatalf("reason = %q, want %q", reason, "cost")
	}
	sess, ok := st.FindSession("game")
	if !ok || sess.PausedReason != "cost" {
		t.Fatalf("PausedReason not persisted: ok=%v sess=%+v", ok, sess)
	}
}

// TestSessionBudgetGate_CheckHeadroom proves the headroom the host watches
// mid-turn is what the cap leaves after the transcript's already-spent tokens,
// that the tightest of the session and cohort caps wins, and that a session
// with no token cap reports no cap at all.
func TestSessionBudgetGate_CheckHeadroom(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{
		{Name: "parent", TokenCap: 1000, CohortTokenCap: 1200},
		{Name: "child", Parent: "parent", TokenCap: 1000, CohortTokenCap: 1200},
		{Name: "free"},
	}
	for _, s := range []struct {
		name string
		in   int
		out  int
	}{{"parent", 200, 100}, {"child", 400, 100}} {
		if err := state.AppendTranscript(state.TranscriptPath(h.PartDir(), s.name), state.TranscriptEntry{
			Ts: "2026-08-04T00:00:00Z", Role: "assistant", Text: "done", TokensIn: s.in, TokensOut: s.out,
		}); err != nil {
			t.Fatal(err)
		}
	}

	gate := h.BudgetGate()

	// child spent 500 of its own 1000 cap; the cohort spent 800 of 1200. The
	// cohort's 400 is tighter than the session's 500, so it is what the turn gets.
	_, got, capped := gate.Check("child")
	if !capped || got != 400 {
		t.Fatalf("Check(child) headroom = (%d, %v), want (400, true)", got, capped)
	}
	if _, got, capped := gate.Check("free"); capped || got != 0 {
		t.Fatalf("Check(free) headroom = (%d, %v), want (0, false)", got, capped)
	}
	if _, got, capped := gate.Check("ghost"); capped || got != 0 {
		t.Fatalf("Check of an unknown session = (%d, %v), want (0, false)", got, capped)
	}
}

// TestSessionBudgetGate_HeadroomFloorsAtZero proves a session already past its
// cap reports no headroom rather than wrapping the unsigned subtraction into an
// enormous one.
func TestSessionBudgetGate_HeadroomFloorsAtZero(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{{Name: "spent", TokenCap: 100}}
	if err := state.AppendTranscript(state.TranscriptPath(h.PartDir(), "spent"), state.TranscriptEntry{
		Ts: "2026-08-04T00:00:00Z", Role: "assistant", Text: "done", TokensIn: 500, TokensOut: 500,
	}); err != nil {
		t.Fatal(err)
	}
	_, got, capped := h.BudgetGate().Check("spent")
	if !capped || got != 0 {
		t.Fatalf("headroom over cap = (%d, %v), want (0, true)", got, capped)
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
	reason, _, _ := gate.Check("free")
	if reason != "" {
		t.Fatalf("reason = %q, want empty", reason)
	}
	sess, ok := st.FindSession("free")
	if !ok || sess.PausedReason != "" {
		t.Fatalf("PausedReason must stay empty: ok=%v sess=%+v", ok, sess)
	}
}

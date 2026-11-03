package manager

import (
	"context"
	"errors"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestBudgetReason(t *testing.T) {
	cases := []struct {
		name           string
		sc             float64
		st             uint64
		cc, ctk        float64
		tkCap          uint64
		coc, coctk     float64
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

func TestSessionBudgetGate_CheckTrips(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{{Name: "game", CostCap: 5}}
	if err := state.AppendTranscript(state.TranscriptPath(h.PartDir(), "game"), state.TranscriptEntry{
		Ts: "2026-07-27T00:00:00Z", Role: "assistant", Text: "done", Cost: 6,
	}); err != nil {
		t.Fatal(err)
	}

	gate := h.BudgetGate()
	reason := gate.Check("game").Reason
	if reason != "cost" {
		t.Fatalf("reason = %q, want %q", reason, "cost")
	}
	sess, ok := st.FindSession("game")
	if !ok || sess.PausedReason != "cost" {
		t.Fatalf("PausedReason not persisted: ok=%v sess=%+v", ok, sess)
	}
}

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

	child := gate.Check("child")
	if !child.Capped || child.Headroom != 400 {
		t.Fatalf("Check(child) headroom = (%d, %v), want (400, true)", child.Headroom, child.Capped)
	}
	if free := gate.Check("free"); free.Capped || free.Headroom != 0 {
		t.Fatalf("Check(free) headroom = (%d, %v), want (0, false)", free.Headroom, free.Capped)
	}
	if ghost := gate.Check("ghost"); ghost.Capped || ghost.Headroom != 0 {
		t.Fatalf("Check of an unknown session = (%d, %v), want (0, false)", ghost.Headroom, ghost.Capped)
	}
}

func TestSessionBudgetGate_HeadroomFloorsAtZero(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{{Name: "spent", TokenCap: 100}}
	if err := state.AppendTranscript(state.TranscriptPath(h.PartDir(), "spent"), state.TranscriptEntry{
		Ts: "2026-08-04T00:00:00Z", Role: "assistant", Text: "done", TokensIn: 500, TokensOut: 500,
	}); err != nil {
		t.Fatal(err)
	}
	spent := h.BudgetGate().Check("spent")
	if !spent.Capped || spent.Headroom != 0 {
		t.Fatalf("headroom over cap = (%d, %v), want (0, true)", spent.Headroom, spent.Capped)
	}
}

func TestSessionBudgetGate_UncappedNeverTrips(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{{Name: "free"}}
	if err := state.AppendTranscript(state.TranscriptPath(h.PartDir(), "free"), state.TranscriptEntry{
		Ts: "2026-07-27T00:00:00Z", Role: "assistant", Text: "done", Cost: 999,
	}); err != nil {
		t.Fatal(err)
	}

	gate := h.BudgetGate()
	reason := gate.Check("free").Reason
	if reason != "" {
		t.Fatalf("reason = %q, want empty", reason)
	}
	sess, ok := st.FindSession("free")
	if !ok || sess.PausedReason != "" {
		t.Fatalf("PausedReason must stay empty: ok=%v sess=%+v", ok, sess)
	}
}

func TestNonNegativeCountClampsNegativeTotals(t *testing.T) {
	if got := nonNegativeCount(-3); got != 0 {
		t.Fatalf("nonNegativeCount(-3) = %d, want 0", got)
	}
	if got := nonNegativeCount(7); got != 7 {
		t.Fatalf("nonNegativeCount(7) = %d, want 7", got)
	}
}

func TestSessionSetBudgetReportsAnUnknownSession(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "category")
	_, err := h.sessionSetBudgetRun(context.Background(), args("name", "ghost", "cost_cap", "5"))
	if _, ok := errors.AsType[*SessionNotFoundError](err); !ok {
		t.Fatalf("err = %v, want a *SessionNotFoundError", err)
	}
}

package manager

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSessionJSONRow_PausedStatusFromReason(t *testing.T) {
	row := sessionJSONRow(state.Session{Name: "game", PausedReason: "cost"})
	if row.Status != "paused" {
		t.Fatalf("status = %q, want paused", row.Status)
	}
}

func TestSessionJSONRow_ArchivedBeatsPaused(t *testing.T) {
	row := sessionJSONRow(state.Session{Name: "game", PausedReason: "cost", Archived: true})
	if row.Status != "archived" {
		t.Fatalf("status = %q, want archived", row.Status)
	}
}

func TestAttachUsage_StampsCapsAndReason(t *testing.T) {
	h := &Handler{}
	row := &sessionJSON{ID: "game", Usage: &usageJSON{Cost: 1.5, Turns: 2}}
	sess := state.Session{Name: "game", CostCap: 5, TokenCap: 100000, PausedReason: "cost"}
	h.stampBudget(row, sess)
	if row.Usage.CostCap != 5 || row.Usage.TokenCap != 100000 {
		t.Fatalf("caps not stamped: %+v", row.Usage)
	}
	if row.Usage.PausedReason != "cost" {
		t.Fatalf("paused_reason = %q, want cost", row.Usage.PausedReason)
	}
}

func TestCohortTotals_SumsParentForest(t *testing.T) {
	all := []state.Session{
		{Name: "lead"},
		{Name: "w1", Parent: "lead"},
		{Name: "w2", Parent: "lead"},
		{Name: "other"},
	}
	usage := map[string]struct {
		c float64
		t uint64
	}{"lead": {1, 10}, "w1": {2, 20}, "w2": {3, 30}, "other": {99, 990}}
	total := cohortTotals(all[1], all, func(s state.Session) sessionUsage {
		u := usage[s.Name]
		return sessionUsage{cost: u.c, tokens: u.t}
	})
	cost, tokens := total.cost, total.tokens
	if cost != 6 || tokens != 60 {
		t.Fatalf("cohort totals = %v/%v, want 6/60", cost, tokens)
	}
}

func TestCohortTotals_SoloSessionIsItself(t *testing.T) {
	all := []state.Session{{Name: "solo"}}
	solo := cohortTotals(all[0], all, func(_ state.Session) sessionUsage {
		return sessionUsage{cost: 4, tokens: 40}
	})
	cost, tokens := solo.cost, solo.tokens
	if cost != 4 || tokens != 40 {
		t.Fatalf("solo cohort = %v/%v, want 4/40", cost, tokens)
	}
}

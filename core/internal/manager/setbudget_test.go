package manager

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestApplyBudget_ClearsPauseWhenCapRaised(t *testing.T) {
	s := state.Session{Name: "game", CostCap: 5, PausedReason: "cost"}
	// Session already spent $6 (over the old $5 cap). Raise cap to $20.
	out := applyBudget(s, budgetPatch{costCap: fptr(20)}, 6.0, 0)
	if out.CostCap != 20 {
		t.Fatalf("cost cap = %v, want 20", out.CostCap)
	}
	if out.PausedReason != "" {
		t.Fatalf("paused reason = %q, want cleared", out.PausedReason)
	}
}

func TestApplyBudget_KeepsPauseWhenStillOverCap(t *testing.T) {
	s := state.Session{Name: "game", CostCap: 5, PausedReason: "cost"}
	out := applyBudget(s, budgetPatch{costCap: fptr(5.5)}, 6.0, 0)
	if out.PausedReason != "cost" {
		t.Fatalf("paused reason = %q, want still cost", out.PausedReason)
	}
}

func TestApplyBudget_ClearCapWithZeroUncaps(t *testing.T) {
	s := state.Session{Name: "game", CostCap: 5, PausedReason: "cost"}
	out := applyBudget(s, budgetPatch{costCap: fptr(0)}, 6.0, 0)
	if out.CostCap != 0 || out.PausedReason != "" {
		t.Fatalf("uncapped clear failed: %+v", out)
	}
}

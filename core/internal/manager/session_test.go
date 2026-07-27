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
	row := &sessionJSON{Id: "game", Usage: &usageJSON{Cost: 1.5, Turns: 2}}
	sess := state.Session{Name: "game", CostCap: 5, TokenCap: 100000, PausedReason: "cost"}
	h.stampBudget(row, sess)
	if row.Usage.CostCap != 5 || row.Usage.TokenCap != 100000 {
		t.Fatalf("caps not stamped: %+v", row.Usage)
	}
	if row.Usage.PausedReason != "cost" {
		t.Fatalf("paused_reason = %q, want cost", row.Usage.PausedReason)
	}
}
